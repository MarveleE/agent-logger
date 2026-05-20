package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/agentlogger/agentlog/internal/api"
	"github.com/agentlogger/agentlog/internal/daemon"
	"github.com/agentlogger/agentlog/internal/store"
)

// Daemon lifecycle commands (start/stop/status) live at the top level —
// see root command in main.go. No "server" subgroup; auto-start is not
// supported, users run `agentlog start` themselves (or via their own
// supervisor — launchd / systemd / nssm / etc).

func newServerStartCmd() *cobra.Command {
	var (
		port    int
		bind    string
		dataDir string
		quiet   bool
	)
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the server in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := daemon.DefaultPaths(dataDir)
			if err != nil {
				return err
			}

			// Refuse to start if another instance is already alive.
			if pid, _ := daemon.ReadPID(paths.PIDFile); pid > 0 && daemon.IsAlive(pid) {
				return fmt.Errorf("server already running (pid %d). Use 'agentlog server stop' first", pid)
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

			go func() {
				<-ctx.Done()
				if !quiet {
					fmt.Fprintln(os.Stderr, "agentlog: shutting down")
				}
			}()

			if !quiet {
				fmt.Fprintf(os.Stderr, "agentlog: listening on http://%s\n", addr)
				fmt.Fprintf(os.Stderr, "agentlog: data dir %s\n", paths.DataDir)
			}
			return srv.ListenAndServe(ctx)
		},
	}
	cmd.Flags().IntVar(&port, "port", 8765, "TCP port to bind")
	cmd.Flags().StringVar(&bind, "bind", "0.0.0.0", "Address to bind. 0.0.0.0 (default) accepts simulator + real-device + LAN connections; use 127.0.0.1 to restrict to local processes only (untrusted networks)")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Override data directory")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Suppress startup messages")
	return cmd
}

func newServerStopCmd() *cobra.Command {
	var (
		dataDir string
		wait    time.Duration
	)
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the running server",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := daemon.DefaultPaths(dataDir)
			if err != nil {
				return err
			}
			if err := daemon.StopProcess(paths); err != nil {
				if errors.Is(err, daemon.ErrNotRunning) {
					fmt.Fprintln(os.Stderr, "server is not running")
					return nil
				}
				return err
			}
			if err := daemon.WaitForStop(paths.PIDFile, wait); err != nil {
				return err
			}
			daemon.RemovePIDFile(paths.PIDFile)
			daemon.RemovePortFile(paths.PortFile)
			fmt.Fprintln(os.Stderr, "server stopped")
			return nil
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Override data directory")
	cmd.Flags().DurationVar(&wait, "wait", 5*time.Second, "Maximum time to wait for shutdown")
	return cmd
}

func newServerStatusCmd() *cobra.Command {
	var dataDir string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show whether the server is running",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := daemon.DefaultPaths(dataDir)
			if err != nil {
				return err
			}
			pid, err := daemon.ReadPID(paths.PIDFile)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if pid > 0 && daemon.IsAlive(pid) {
				fmt.Fprintf(out, "running\npid=%d\ndata=%s\n", pid, paths.DataDir)
				return nil
			}
			fmt.Fprintln(out, "stopped")
			return nil
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Override data directory")
	return cmd
}
