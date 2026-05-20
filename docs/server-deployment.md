# Server deployment

The agentlog daemon is a single binary with no auto-start machinery built in.
Run it however your workflow prefers:

```bash
agentlog start &                       # background in current shell
nohup agentlog start &                 # survives shell exit
make server-start                      # foreground, dev convenience
```

For something supervised (auto-restart on crash, start on login), wrap it in
your platform's supervisor — `launchd` on macOS, `systemd` user unit on Linux,
NSSM or `sc.exe` on Windows.

Data lives in `~/.agentlog/`:

- `agentlog.sqlite` — the database (WAL mode + FTS5)
- `agentlog.pid`    — running pid, used by `start`/`stop`/`status`
- `agentlog.port`   — bound port, used by `stop` on Windows
- `server.log`      — only present if you redirected stdout/stderr to it

Stop with `agentlog stop`. Inspect with `agentlog status` and `agentlog instances list`.
