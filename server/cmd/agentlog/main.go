// Command agentlog is the AgentLogger server + CLI.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/agentlogger/agentlog/internal/client"
)

// version is set via -ldflags at build time.
var version = "0.0.0-dev"

// rootLong is the comprehensive operation cheatsheet shown in `agentlog --help`.
// Keep it grouped by use case so an agent or human can scan it once and find
// every command without diving into subgroup `--help`.
const rootLong = `AgentLogger — local log capture daemon and query CLI for apps shipping logs over HTTP.

Common operations (every executable command, grouped by use case):

  Daemon lifecycle:
    agentlog start [--port N] [--bind ADDR] [--data-dir DIR]   Start the daemon in foreground
    agentlog stop  [--data-dir DIR]                            Stop the running daemon (graceful)
    agentlog status [--data-dir DIR]                           Show pid + data dir
    agentlog dev   [--bundle ID] [--level LV]                  Start daemon + tail in one shot (Ctrl-C stops both)

  Multi-instance (multiple daemons on different ports / data-dirs):
    agentlog instances list [--json] [--scan DIR]              List every live daemon on this machine
    agentlog instances stop-all [--dry-run]                    Stop every daemon

  Sessions (one per app launch):
    agentlog sessions list [--bundle ID] [--platform P] [--active] [--json]
    agentlog sessions latest --bundle ID [--json]              Most recent session for a bundle
    agentlog sessions show <id> [--json]                       Full details for one session

  Logs:
    agentlog logs   [--session ID | --bundle ID] [--level LV] [--category CAT]
                    [--since DURATION | --from MS --to MS] [--grep PATTERN]
                    [--order asc|desc] [--limit N] [--json]
    agentlog tail   [--session ID | --bundle ID] [--level LV] [--json] [--interval D]
    agentlog search "<query>" [--bundle ID] [--limit N] [--json]   Full-text (FTS5) search

  Database maintenance:
    agentlog db prune --before 7d                              Delete log entries older than DURATION
    agentlog db reset [--yes]                                  Drop every session and log

  Misc:
    agentlog version                                           Print build version
    agentlog completion [bash|zsh|fish|powershell]             Generate shell completion script

Global flag:
  --endpoint URL      Pick the daemon (default $AGENTLOGGER_ENDPOINT or http://127.0.0.1:8765)

Append --json to any read command for NDJSON output (one record per line).
Run 'agentlog <command> --help' for the full flag list on any subcommand.`

func main() {
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "agentlog",
		Short:         "AgentLogger — local log capture daemon and query CLI for apps shipping logs over HTTP",
		Long:          rootLong,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("endpoint", endpointFromEnv(), "agentlog server endpoint")

	root.AddCommand(newVersionCmd())
	// Daemon lifecycle at the top level — there is no auto-start; users
	// run `agentlog start` themselves or wrap it in their own supervisor.
	root.AddCommand(newServerStartCmd())
	root.AddCommand(newServerStopCmd())
	root.AddCommand(newServerStatusCmd())
	root.AddCommand(newDevCmd())
	root.AddCommand(newInstancesCmd())
	root.AddCommand(newSessionsCmd())
	root.AddCommand(newLogsCmd())
	root.AddCommand(newTailCmd())
	root.AddCommand(newSearchCmd())
	root.AddCommand(newDBCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version)
		},
	}
}

// newClient returns a configured API client. Endpoint precedence:
//
//	--endpoint flag > AGENTLOGGER_ENDPOINT env > default (127.0.0.1:8765)
func newClient(cmd *cobra.Command) *client.Client {
	ep, _ := cmd.Flags().GetString("endpoint")
	if ep == "" {
		ep = client.DefaultEndpoint
	}
	return client.New(ep)
}

func endpointFromEnv() string {
	if v := os.Getenv("AGENTLOGGER_ENDPOINT"); v != "" {
		return v
	}
	return client.DefaultEndpoint
}
