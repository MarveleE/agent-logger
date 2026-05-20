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

func newServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage the agentlog server",
	}
	cmd.AddCommand(newServerStartCmd())
	cmd.AddCommand(newServerStopCmd())
	cmd.AddCommand(newServerStatusCmd())
	cmd.AddCommand(newServerInstallCmd())
	cmd.AddCommand(newServerUninstallCmd())
	return cmd
}

func newServerInstallCmd() *cobra.Command {
	var (
		port    int
		bind    string
		dataDir string
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install launchd plist so the server auto-starts on login",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := daemon.DefaultPaths(dataDir)
			if err != nil {
				return err
			}
			binary, err := os.Executable()
			if err != nil {
				return err
			}
			if err := daemon.AutoInstall(binary, port, bind, paths); err != nil {
				if errors.Is(err, daemon.ErrNotSupported) {
					fmt.Fprintln(os.Stderr, "agentlog: auto-start is not supported on this platform; start the daemon manually or via systemd / sc.exe")
					return nil
				}
				return err
			}
			path, _ := daemon.AutostartPath()
			fmt.Fprintf(os.Stderr, "agentlog: installed %s (%s)\n", path, daemon.AutoKind())
			fmt.Fprintf(os.Stderr, "agentlog: will listen on http://%s:%d on next login\n", bind, port)
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 8765, "TCP port to bind")
	cmd.Flags().StringVar(&bind, "bind", "0.0.0.0", "Address to bind (0.0.0.0 enables real-device access)")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Override data directory")
	return cmd
}

func newServerUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the auto-start entry installed by 'server install'",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := daemon.AutoUninstall(); err != nil {
				if errors.Is(err, daemon.ErrNotSupported) {
					fmt.Fprintln(os.Stderr, "agentlog: auto-start is not supported on this platform")
					return nil
				}
				return err
			}
			fmt.Fprintln(os.Stderr, "agentlog: auto-start entry removed")
			return nil
		},
	}
	return cmd
}

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
	cmd.Flags().StringVar(&bind, "bind", "127.0.0.1", "Address to bind (use 0.0.0.0 for real-device support)")
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
			installed, _ := daemon.AutoStatus()
			out := cmd.OutOrStdout()
			if pid > 0 && daemon.IsAlive(pid) {
				fmt.Fprintf(out, "running\npid=%d\ndata=%s\nautostart=%v (%s)\n",
					pid, paths.DataDir, installed, daemon.AutoKind())
				return nil
			}
			fmt.Fprintf(out, "stopped\nautostart=%v (%s)\n", installed, daemon.AutoKind())
			return nil
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Override data directory")
	return cmd
}
