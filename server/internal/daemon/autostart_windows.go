//go:build windows

package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// On Windows we drop a .bat into the per-user Startup folder. It runs on
// every login as the current user — no admin rights, no service install.
// Crash recovery is NOT provided (Startup files only fire on login). For
// supervised behavior, install agentlog.exe as a Windows Service via sc.exe
// or a tool like NSSM separately.

const autostartBasename = "agentlogger.bat"

// AutostartPath returns the .bat path inside %APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup.
func AutostartPath() (string, error) {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		return "", errors.New("APPDATA environment variable not set")
	}
	return filepath.Join(appdata,
		"Microsoft", "Windows", "Start Menu", "Programs", "Startup",
		autostartBasename), nil
}

// AutoInstall writes the Startup-folder .bat so the daemon starts on login.
func AutoInstall(binary string, port int, bind string, paths *Paths) error {
	if !filepath.IsAbs(binary) {
		abs, err := filepath.Abs(binary)
		if err != nil {
			return err
		}
		binary = abs
	}
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("binary %s not found: %w", binary, err)
	}

	dst, err := AutostartPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	content := buildStartupBat(binary, port, bind, paths)
	return os.WriteFile(dst, []byte(content), 0o644)
}

// AutoUninstall removes the Startup-folder .bat.
func AutoUninstall() error {
	dst, err := AutostartPath()
	if err != nil {
		return err
	}
	if err := os.Remove(dst); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// AutoStatus returns true if the Startup-folder entry exists.
func AutoStatus() (bool, error) {
	dst, err := AutostartPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(dst)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// AutoKind returns the name of the underlying mechanism (informational).
func AutoKind() string { return "startup-folder" }

func buildStartupBat(binary string, port int, bind string, paths *Paths) string {
	// `start "" /B` detaches and runs in the background without opening a window.
	// stdout/stderr are redirected to server.log inside the data dir.
	return `@echo off
start "" /B "` + binary + `" server start ` +
		`--port ` + strconv.Itoa(port) + ` ` +
		`--bind ` + bind + ` ` +
		`--data-dir "` + paths.DataDir + `" ` +
		`--quiet ` +
		`>> "` + paths.LogFile + `" 2>&1
`
}
