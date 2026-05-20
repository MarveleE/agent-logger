package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/agentlogger/agentlog/internal/api"
	"github.com/agentlogger/agentlog/internal/client"
	"github.com/agentlogger/agentlog/internal/daemon"
	"github.com/agentlogger/agentlog/internal/store"
)

// newDevCmd implements `agentlog dev`: start the daemon AND tail every
// incoming log line in one foreground process. Ctrl-C stops both.
//
// It's the convenient "I'm debugging right now" mode. For production-style
// long-running daemons, use `agentlog start &` so the daemon stays up after
// the shell closes.
func newDevCmd() *cobra.Command {
	var (
		port     int
		bind     string
		dataDir  string
		bundle   string
		level    string
		interval time.Duration
		asJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Start the daemon and tail every incoming log in one process (Ctrl-C stops both)",
		Long: `Start the daemon in foreground and immediately stream every log line that
arrives — convenient for active debugging. Ctrl-C stops the tail and the
daemon together.

By default no bundle filter is applied, so logs from every app shipping to
this daemon scroll through. Use --bundle to narrow.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := daemon.DefaultPaths(dataDir)
			if err != nil {
				return err
			}
			if pid, _ := daemon.ReadPID(paths.PIDFile); pid > 0 && daemon.IsAlive(pid) {
				return fmt.Errorf("a daemon is already running (pid %d, data %s). 'agentlog stop' it first, "+
					"or use a different --data-dir + --port for an isolated dev instance", pid, paths.DataDir)
			}

			st, err := store.Open(paths.DBFile)
			if err != nil {
				return err
			}
			defer st.Close()

			addr := fmt.Sprintf("%s:%d", bind, port)
			srv := api.New(st, addr)

			if err := daemon.WritePIDFile(paths.PIDFile); err != nil {
				return err
			}
			defer daemon.RemovePIDFile(paths.PIDFile)
			if err := daemon.WritePortFile(paths.PortFile, port); err != nil {
				return err
			}
			defer daemon.RemovePortFile(paths.PortFile)

			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			errOut := cmd.ErrOrStderr()
			out := cmd.OutOrStdout()

			fmt.Fprintf(errOut, "agentlog dev: daemon on http://%s · data %s\n", addr, paths.DataDir)
			if bundle != "" {
				fmt.Fprintf(errOut, "agentlog dev: tailing bundle=%s\n", bundle)
			} else {
				fmt.Fprintln(errOut, "agentlog dev: tailing every bundle (use --bundle to narrow)")
			}
			fmt.Fprintln(errOut, "(Ctrl-C to stop daemon + tail)")
			fmt.Fprintln(errOut)

			daemonErr := make(chan error, 1)
			go func() { daemonErr <- srv.ListenAndServe(ctx) }()

			// Probe /v1/health a few times so we don't race the listener.
			c := client.New("http://" + addr)
			ready := false
			for i := 0; i < 50 && !ready; i++ {
				if c.Health(ctx) == nil {
					ready = true
					break
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(50 * time.Millisecond):
				}
			}
			if !ready {
				cancel()
				return errors.New("daemon did not come up within 2.5s")
			}

			// Cursor-based polling tail across all sessions (or one bundle if filtered).
			cache := newSessionCache(c)
			// Anchor cursor to the current tail of the DB so reusing an existing
			// data dir doesn't dump pre-existing logs on startup. dev is for
			// watching live activity; use `agentlog logs` to inspect history.
			var cursor int64
			if head, err := c.QueryLogs(ctx, client.QueryLogsOpts{
				Bundle: bundle,
				Order:  "desc",
				Limit:  1,
			}); err == nil && len(head) > 0 {
				cursor = head[0].ID
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil
				case err := <-daemonErr:
					if err != nil {
						return fmt.Errorf("daemon exited: %w", err)
					}
					return nil
				case <-ticker.C:
					batch, err := c.QueryLogs(ctx, client.QueryLogsOpts{
						Bundle: bundle,
						Level:  level,
						Order:  "asc",
						Cursor: cursor,
						Limit:  500,
					})
					if err != nil {
						if errors.Is(err, context.Canceled) {
							return nil
						}
						continue
					}
					for i := range batch {
						_ = renderOne(out, asJSON, &batch[i], cache.get(ctx, batch[i].SessionID))
						if batch[i].ID > cursor {
							cursor = batch[i].ID
						}
					}
				}
			}
		},
	}
	cmd.Flags().IntVar(&port, "port", 8765, "TCP port to bind")
	cmd.Flags().StringVar(&bind, "bind", "0.0.0.0", "Address to bind. 0.0.0.0 (default) accepts simulator + real-device + LAN; use 127.0.0.1 on untrusted networks")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Override data directory")
	cmd.Flags().StringVar(&bundle, "bundle", "", "Tail only this bundle id (default: all)")
	cmd.Flags().StringVar(&level, "level", "", "Minimum log level filter (trace|debug|info|notice|warning|error|critical)")
	cmd.Flags().DurationVar(&interval, "interval", 250*time.Millisecond, "Poll interval")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as NDJSON")
	return cmd
}
