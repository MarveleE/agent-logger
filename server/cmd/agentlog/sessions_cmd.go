package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/agentlogger/agentlog/internal/client"
)

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Inspect captured sessions",
	}
	cmd.AddCommand(newSessionsListCmd())
	cmd.AddCommand(newSessionsLatestCmd())
	cmd.AddCommand(newSessionsShowCmd())
	return cmd
}

func newSessionsListCmd() *cobra.Command {
	var (
		bundle, device, platform, activeWithin string
		active                                 bool
		limit                                  int
		asJSON                                 bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(cmd)
			sessions, err := c.ListSessions(cmd.Context(), client.ListSessionsOpts{
				Bundle:       bundle,
				Device:       device,
				Platform:     platform,
				Active:       active,
				ActiveWithin: activeWithin,
				Limit:        limit,
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				for i := range sessions {
					if err := writeNDJSON(out, &sessions[i]); err != nil {
						return err
					}
				}
				return nil
			}
			if len(sessions) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "(no sessions)")
				return nil
			}
			for i := range sessions {
				fmt.Fprintln(out, formatSessionText(&sessions[i]))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&bundle, "bundle", "", "Filter by bundle id (logical app id)")
	cmd.Flags().StringVar(&device, "device", "", "Filter by device id")
	cmd.Flags().StringVar(&platform, "platform", "", "Filter by platform (ios|android|macos|web|node|…)")
	cmd.Flags().BoolVar(&active, "active", false, "Only sessions that posted recently")
	cmd.Flags().StringVar(&activeWithin, "active-within", "5m", "Window for --active (e.g. 30s, 5m)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum sessions to return")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as NDJSON (one session per line)")
	return cmd
}

func newSessionsLatestCmd() *cobra.Command {
	var (
		bundle string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "latest",
		Short: "Get the most recent session for a bundle id",
		RunE: func(cmd *cobra.Command, args []string) error {
			if bundle == "" {
				return errors.New("--bundle is required")
			}
			c := newClient(cmd)
			sess, err := c.LatestSession(cmd.Context(), bundle)
			if err != nil {
				if errors.Is(err, client.ErrNotFound) {
					return errSessionNotFound
				}
				return err
			}
			if asJSON {
				return writeNDJSON(cmd.OutOrStdout(), sess)
			}
			fmt.Fprintln(cmd.OutOrStdout(), formatSessionText(sess))
			return nil
		},
	}
	cmd.Flags().StringVar(&bundle, "bundle", "", "Bundle id (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output the session as JSON")
	return cmd
}

func newSessionsShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show details for a single session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(cmd)
			sess, err := c.GetSession(cmd.Context(), args[0])
			if err != nil {
				if errors.Is(err, client.ErrNotFound) {
					return errSessionNotFound
				}
				return err
			}
			if asJSON {
				return writeNDJSON(cmd.OutOrStdout(), sess)
			}
			fmt.Fprintln(cmd.OutOrStdout(), formatSessionText(sess))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

// errSessionNotFound is returned by sessions latest/show when no record matches.
var errSessionNotFound = errors.New("no session found")
