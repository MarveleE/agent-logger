# Server deployment

(Placeholder — Phase 6 will document launchd setup, data dir layout, and config.)

Data lives in `~/.agentlogger/`:

- `agentlog.sqlite` — the database (WAL mode)
- `agentlog.pid` — running server pid file
- `server.log` — server output (when started via launchd)
- `config.toml` — optional overrides (port, bind address, retention)
