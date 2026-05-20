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

func main() {
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "agentlog",
		Short:         "AgentLogger — iOS log capture for xcodebuild / simctl workflows",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("endpoint", endpointFromEnv(), "agentlog server endpoint")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newServerCmd())
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
