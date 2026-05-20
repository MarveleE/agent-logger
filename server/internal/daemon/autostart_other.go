//go:build !darwin && !windows

package daemon

// Other platforms (Linux, BSD) — no auto-install support in v1. Users can
// wire systemd / SysV / OpenRC on their own. The daemon still runs fine in
// foreground via `agentlog server start`.

func AutostartPath() (string, error) { return "", ErrNotSupported }
func AutoInstall(binary string, port int, bind string, paths *Paths) error {
	return ErrNotSupported
}
func AutoUninstall() error           { return ErrNotSupported }
func AutoStatus() (bool, error)      { return false, nil }
func AutoKind() string               { return "unsupported" }
