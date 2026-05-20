package main

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newDBCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database maintenance",
	}
	cmd.AddCommand(newDBPruneCmd())
	cmd.AddCommand(newDBResetCmd())
	return cmd
}

func newDBPruneCmd() *cobra.Command {
	var before string
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete log entries older than --before",
		RunE: func(cmd *cobra.Command, args []string) error {
			if before == "" {
				return errors.New("--before is required (e.g. 7d, 24h)")
			}
			c := newClient(cmd)
			n, err := c.Prune(cmd.Context(), before)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "pruned %d log entries older than %s\n", n, before)
			return nil
		},
	}
	cmd.Flags().StringVar(&before, "before", "", "Cutoff (Go duration like 7d, 12h, or unix ms)")
	return cmd
}

func newDBResetCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Delete all sessions and logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				fmt.Fprint(cmd.ErrOrStderr(), "About to delete ALL sessions and logs. Type 'yes' to confirm: ")
				line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if strings.TrimSpace(line) != "yes" {
					fmt.Fprintln(cmd.ErrOrStderr(), "aborted")
					return nil
				}
			}
			c := newClient(cmd)
			if err := c.Reset(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "database reset")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}
