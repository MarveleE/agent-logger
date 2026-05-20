package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/agentlogger/agentlog/internal/client"
	"github.com/agentlogger/agentlog/internal/model"
)

func newLogsCmd() *cobra.Command {
	var (
		session, bundle, level, category, since, grep, order string
		from, to                                              int64
		limit                                                 int
		asJSON                                                bool
	)
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Query log entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(cmd)
			logs, err := c.QueryLogs(cmd.Context(), client.QueryLogsOpts{
				Session: session, Bundle: bundle, Level: level, Category: category,
				Since: since, From: from, To: to, Grep: grep, Limit: limit, Order: order,
			})
			if err != nil {
				return err
			}
			return renderLogs(cmd.OutOrStdout(), asJSON, logs)
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Filter by session id")
	cmd.Flags().StringVar(&bundle, "bundle", "", "Filter by bundle id")
	cmd.Flags().StringVar(&level, "level", "", "Minimum level (trace|debug|info|notice|warning|error|critical)")
	cmd.Flags().StringVar(&category, "category", "", "Filter by category")
	cmd.Flags().StringVar(&since, "since", "", "Logs newer than e.g. 5m, 30s, 2h")
	cmd.Flags().Int64Var(&from, "from", 0, "Lower bound unix ms (exclusive of --since)")
	cmd.Flags().Int64Var(&to, "to", 0, "Upper bound unix ms")
	cmd.Flags().StringVar(&grep, "grep", "", "Substring match against message")
	cmd.Flags().IntVar(&limit, "limit", 200, "Maximum entries to return")
	cmd.Flags().StringVar(&order, "order", "desc", "Sort order: asc | desc")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as NDJSON")
	return cmd
}

func newTailCmd() *cobra.Command {
	var (
		session, bundle, level string
		asJSON                 bool
		interval               time.Duration
	)
	cmd := &cobra.Command{
		Use:   "tail",
		Short: "Stream new log entries (defaults to latest session for --bundle)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(cmd)
			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()

			if session == "" {
				if bundle == "" {
					return errors.New("provide --session or --bundle")
				}
				sess, err := c.LatestSession(cmd.Context(), bundle)
				if err != nil {
					return err
				}
				session = sess.ID
				fmt.Fprintf(errOut, "agentlog: tailing session %s (%s on %s)\n", sess.ID, sess.BundleID, defaultStr(sess.DeviceName, sess.DeviceID))
			}

			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			seed, err := c.QueryLogs(ctx, client.QueryLogsOpts{
				Session: session, Level: level, Limit: 20, Order: "desc",
			})
			if err != nil {
				return err
			}
			for i := len(seed) - 1; i >= 0; i-- {
				_ = renderOne(out, asJSON, &seed[i])
			}
			var cursor int64
			if len(seed) > 0 {
				cursor = seed[0].ID
			}

			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
					batch, err := c.QueryLogs(ctx, client.QueryLogsOpts{
						Session: session, Level: level, Order: "asc", Cursor: cursor, Limit: 500,
					})
					if err != nil {
						if errors.Is(err, context.Canceled) {
							return nil
						}
						return err
					}
					for i := range batch {
						_ = renderOne(out, asJSON, &batch[i])
						if batch[i].ID > cursor {
							cursor = batch[i].ID
						}
					}
				}
			}
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Session id to tail")
	cmd.Flags().StringVar(&bundle, "bundle", "", "Bundle id (resolves to latest session)")
	cmd.Flags().StringVar(&level, "level", "", "Minimum level filter")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as NDJSON")
	cmd.Flags().DurationVar(&interval, "interval", 250*time.Millisecond, "Poll interval")
	return cmd
}

func newSearchCmd() *cobra.Command {
	var (
		bundle string
		limit  int
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search across log messages (FTS5)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(cmd)
			logs, err := c.SearchLogs(cmd.Context(), args[0], bundle, limit)
			if err != nil {
				return err
			}
			return renderLogs(cmd.OutOrStdout(), asJSON, logs)
		},
	}
	cmd.Flags().StringVar(&bundle, "bundle", "", "Restrict to bundle id")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum results")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as NDJSON")
	return cmd
}

func renderLogs(out io.Writer, asJSON bool, logs []model.LogEntry) error {
	for i := range logs {
		if err := renderOne(out, asJSON, &logs[i]); err != nil {
			return err
		}
	}
	return nil
}

func renderOne(out io.Writer, asJSON bool, e *model.LogEntry) error {
	if asJSON {
		return writeNDJSON(out, e)
	}
	fmt.Fprintln(out, formatLogText(e))
	return nil
}
