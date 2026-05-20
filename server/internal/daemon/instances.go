package daemon

import (
	"os"
	"path/filepath"
	"time"
)

// Instance describes one live agentlog daemon process found by scanning
// well-known data-directory locations.
type Instance struct {
	DataDir   string    `json:"dataDir"`
	PID       int       `json:"pid"`
	Port      int       `json:"port"`       // 0 if the port file is missing
	StartedAt time.Time `json:"startedAt"`  // pid-file modification time
}

// ListRunning returns every live daemon discoverable via the standard
// data-directory conventions:
//
//   - the canonical $HOME/.agentlog directory
//   - $HOME/.agentlog-*  (per-developer / per-project variants)
//   - $TMPDIR/agentlog-*    (test fixtures and one-off instances)
//   - any paths passed in extraDirs
//
// Liveness is verified by re-reading each pid file and probing the OS.
// Stale pid files (process gone) are skipped silently.
func ListRunning(extraDirs ...string) ([]Instance, error) {
	candidates := make([]string, 0, 8)

	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, DefaultDirName))
		if matches, _ := filepath.Glob(filepath.Join(home, DefaultDirName+"-*")); len(matches) > 0 {
			candidates = append(candidates, matches...)
		}
	}
	// Probe both `$TMPDIR/agentlog-*` (macOS uses /var/folders/.../T) and
	// the conventional `/tmp/agentlog-*` since scripts often hard-code /tmp.
	tmpDirs := []string{os.TempDir(), "/tmp"}
	for _, root := range tmpDirs {
		for _, pat := range []string{"agentlog-*", "agentlogger-*"} {
			if matches, _ := filepath.Glob(filepath.Join(root, pat)); len(matches) > 0 {
				candidates = append(candidates, matches...)
			}
		}
	}
	candidates = append(candidates, extraDirs...)

	seen := make(map[string]struct{}, len(candidates))
	var out []Instance
	for _, dir := range candidates {
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if _, dup := seen[abs]; dup {
			continue
		}
		seen[abs] = struct{}{}

		pidFile := filepath.Join(abs, "agentlog.pid")
		pid, err := ReadPID(pidFile)
		if err != nil || pid == 0 || !IsAlive(pid) {
			continue
		}
		port, _ := ReadPort(filepath.Join(abs, "agentlog.port"))

		var startedAt time.Time
		if info, err := os.Stat(pidFile); err == nil {
			startedAt = info.ModTime()
		}

		out = append(out, Instance{
			DataDir:   abs,
			PID:       pid,
			Port:      port,
			StartedAt: startedAt,
		})
	}
	return out, nil
}
