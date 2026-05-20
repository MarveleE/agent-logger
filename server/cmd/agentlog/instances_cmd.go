package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/agentlogger/agentlog/internal/daemon"
)

func newInstancesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instances",
		Short: "List or manage all running agentlog daemons on this machine",
	}
	cmd.AddCommand(newInstancesListCmd())
	cmd.AddCommand(newInstancesStopAllCmd())
	return cmd
}

func newInstancesListCmd() *cobra.Command {
	var (
		asJSON    bool
		extraDirs []string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every live agentlog daemon discoverable via pid+port files",
		RunE: func(cmd *cobra.Command, args []string) error {
			instances, err := daemon.ListRunning(extraDirs...)
			if err != nil {
				return err
			}
			sort.Slice(instances, func(i, j int) bool { return instances[i].Port < instances[j].Port })

			out := cmd.OutOrStdout()
			if asJSON {
				for i := range instances {
					if err := writeNDJSON(out, &instances[i]); err != nil {
						return err
					}
				}
				return nil
			}
			if len(instances) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "(no running daemons)")
				return nil
			}
			for _, inst := range instances {
				started := "?"
				if !inst.StartedAt.IsZero() {
					started = inst.StartedAt.Format(time.RFC3339)
				}
				port := "?"
				if inst.Port > 0 {
					port = fmt.Sprintf("%d", inst.Port)
				}
				fmt.Fprintf(out, "pid=%-7d port=%-6s started=%s data=%s\n",
					inst.PID, port, started, inst.DataDir)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as NDJSON (one record per line)")
	cmd.Flags().StringSliceVar(&extraDirs, "scan", nil, "Extra data-dir paths to probe (repeatable)")
	return cmd
}

func newInstancesStopAllCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "stop-all",
		Short: "Stop every daemon that 'instances list' reports as running",
		RunE: func(cmd *cobra.Command, args []string) error {
			instances, err := daemon.ListRunning()
			if err != nil {
				return err
			}
			if len(instances) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "(no running daemons)")
				return nil
			}
			for _, inst := range instances {
				if dryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "would stop pid=%d data=%s\n", inst.PID, inst.DataDir)
					continue
				}
				paths := &daemon.Paths{
					PIDFile:  inst.DataDir + "/agentlog.pid",
					PortFile: inst.DataDir + "/agentlog.port",
				}
				if err := daemon.StopProcess(paths); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "  pid=%d: %v\n", inst.PID, err)
					continue
				}
				_ = daemon.WaitForStop(paths.PIDFile, 5*time.Second)
				daemon.RemovePIDFile(paths.PIDFile)
				daemon.RemovePortFile(paths.PortFile)
				fmt.Fprintf(cmd.OutOrStdout(), "stopped pid=%d data=%s\n", inst.PID, inst.DataDir)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be stopped without doing it")
	return cmd
}

