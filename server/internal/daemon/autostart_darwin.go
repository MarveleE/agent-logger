//go:build darwin

package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// autostartLabel is the launchd job identifier.
const autostartLabel = "com.agentlogger.daemon"

// AutostartPath returns the per-user launchd plist path.
func AutostartPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", autostartLabel+".plist"), nil
}

// AutoInstall writes the launchd plist for the agentlog server and loads it.
// `binary` is the absolute path to the agentlog executable.
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

	plistPath, err := AutostartPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}

	plist := buildLaunchdPlist(binary, port, bind, paths)
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return err
	}

	_ = exec.Command("launchctl", "unload", plistPath).Run()
	out, err := exec.Command("launchctl", "load", "-w", plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl load: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// AutoUninstall unloads and removes the launchd plist.
func AutoUninstall() error {
	plistPath, err := AutostartPath()
	if err != nil {
		return err
	}
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if err := os.Remove(plistPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// AutoStatus reports whether the launchd job is currently loaded.
func AutoStatus() (bool, error) {
	out, err := exec.Command("launchctl", "list", autostartLabel).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Could not find") {
			return false, nil
		}
		return false, nil
	}
	return strings.Contains(string(out), autostartLabel), nil
}

// AutoKind returns the name of the underlying mechanism (informational).
func AutoKind() string { return "launchd" }

func buildLaunchdPlist(binary string, port int, bind string, paths *Paths) string {
	logPath := paths.LogFile
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>` + autostartLabel + `</string>
    <key>ProgramArguments</key>
    <array>
        <string>` + binary + `</string>
        <string>server</string>
        <string>start</string>
        <string>--port</string>
        <string>` + strconv.Itoa(port) + `</string>
        <string>--bind</string>
        <string>` + bind + `</string>
        <string>--quiet</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>` + logPath + `</string>
    <key>StandardErrorPath</key>
    <string>` + logPath + `</string>
    <key>ProcessType</key>
    <string>Background</string>
</dict>
</plist>
`
}
