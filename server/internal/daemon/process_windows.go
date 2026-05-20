//go:build windows

package daemon

import (
	"fmt"
	"net/http"
	"time"

	"golang.org/x/sys/windows"
)

// stillActive matches Windows' STILL_ACTIVE pseudo-exit-code.
const stillActive = 259

// IsAlive reports whether a process with the given PID is currently running.
// Windows' os.FindProcess always succeeds even for dead PIDs, so we go
// through OpenProcess + GetExitCodeProcess directly.
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// stopRunning asks the live server to shut down via the loopback HTTP
// endpoint. Windows has no signal that lets a console process clean up,
// so we drive the shutdown through the existing HTTP surface.
func stopRunning(_ int, paths *Paths) error {
	port, err := ReadPort(paths.PortFile)
	if err != nil {
		return err
	}
	if port == 0 {
		return fmt.Errorf("port file %s is missing — cannot reach the running server", paths.PortFile)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/internal/shutdown", port)
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post shutdown: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("shutdown returned %d", resp.StatusCode)
	}
	return nil
}
