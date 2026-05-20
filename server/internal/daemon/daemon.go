// Package daemon provides PID-file based lifecycle helpers for the agentlog
// server process. Process-control primitives (IsAlive, StopProcess) are
// platform specific and live in sibling files guarded by build tags.
package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Paths bundles the well-known files under the data directory.
type Paths struct {
	DataDir  string
	DBFile   string
	PIDFile  string
	PortFile string
	LogFile  string
	ConfFile string
}

// DefaultDirName is the dotted directory created under $HOME for data + state.
// Matches the `agentlog` binary name for consistency.
const DefaultDirName = ".agentlog"

// DefaultPaths constructs the conventional layout under $HOME (or override).
// Works on every OS via filepath.Join + os.UserHomeDir.
func DefaultPaths(override string) (*Paths, error) {
	dir := override
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, DefaultDirName)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Paths{
		DataDir:  dir,
		DBFile:   filepath.Join(dir, "agentlog.sqlite"),
		PIDFile:  filepath.Join(dir, "agentlog.pid"),
		PortFile: filepath.Join(dir, "agentlog.port"),
		LogFile:  filepath.Join(dir, "server.log"),
		ConfFile: filepath.Join(dir, "config.toml"),
	}, nil
}

// WritePIDFile writes the current PID to path, replacing any existing file.
func WritePIDFile(path string) error {
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644)
}

// RemovePIDFile deletes the pid file if it exists.
func RemovePIDFile(path string) {
	_ = os.Remove(path)
}

// ReadPID returns the PID stored in the file, or 0 if the file is missing.
func ReadPID(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, fmt.Errorf("malformed pid file %s: %w", path, err)
	}
	return pid, nil
}

// WritePortFile records the TCP port the running server is bound to. The
// stop subcommand reads it to find the HTTP shutdown endpoint on platforms
// where signal-based stop is not available.
func WritePortFile(path string, port int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(port)), 0o644)
}

// RemovePortFile deletes the port file if it exists.
func RemovePortFile(path string) {
	_ = os.Remove(path)
}

// ReadPort returns the port from the file, or 0 if missing.
func ReadPort(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, fmt.Errorf("malformed port file %s: %w", path, err)
	}
	return port, nil
}

// WaitForStop polls until the process referenced by the pid file disappears
// or the timeout expires.
func WaitForStop(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pid, err := ReadPID(path)
		if err != nil {
			return err
		}
		if pid == 0 || !IsAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return ErrTimeout
}

// StopProcess asks the running server (identified by paths) to shut down
// gracefully. The implementation is OS-specific: Unix sends SIGTERM, Windows
// posts to the loopback HTTP shutdown endpoint. Returns ErrNotRunning when
// no live process is found.
func StopProcess(paths *Paths) error {
	pid, err := ReadPID(paths.PIDFile)
	if err != nil {
		return err
	}
	if pid == 0 || !IsAlive(pid) {
		return ErrNotRunning
	}
	return stopRunning(pid, paths)
}

// ErrNotRunning indicates no live process was found via the pid file.
var ErrNotRunning = errors.New("server is not running")

// ErrTimeout indicates a wait operation exceeded its budget.
var ErrTimeout = errors.New("timed out waiting for stop")

