<h1 align="center">AgentLogger</h1>

<p align="center">
  <b>Ship app logs to a local daemon, query them from a CLI.</b><br>
  Built for AI agents that drive <code>xcodebuild</code> / <code>simctl</code> and can't see Xcode's Console.
</p>

<p align="center">
  <img alt="status" src="https://img.shields.io/badge/status-alpha-orange">
  <img alt="server" src="https://img.shields.io/badge/server-macOS%20%7C%20Windows%20%7C%20Linux-blue">
  <img alt="sdk" src="https://img.shields.io/badge/sdk-iOS%2013%2B%20%7C%20macOS%2010.15%2B-blue">
  <img alt="license" src="https://img.shields.io/badge/license-TBD-lightgrey">
</p>

---

## Quick start

```bash
# 1. Install the daemon (binary → /usr/local/bin, launchd auto-start, agent skill)
./scripts/install.sh --from-source

# 2. Verify
agentlog server status        # → running

# 3. Smoke test with the bundled demo (boots iPhone Simulator first if needed)
open -a Simulator
make demo-run

# 4. Read its logs
agentlog tail --bundle com.agentlogger.demo
```

That's it. The daemon stays up via launchd; every iOS launch that bootstraps the SDK appears in `agentlog sessions list`.

---

## Add to an existing iOS app

**Step 1 — depend on the package.** In Xcode: *File → Add Package Dependencies → Add Local…* and point at this repo's `sdks/swift/` folder. Or in `Package.swift`:

```swift
.package(url: "https://github.com/<your-org>/agentlogger-swift.git", from: "0.1.0")
```

**Step 2 — bootstrap once at startup.**

```swift
import AgentLogger

@main
struct MyApp: App {
    init() {
        #if DEBUG
        AgentLogger.bootstrap()
        #else
        AgentLogger.bootstrap(.disabled)
        #endif
    }
    var body: some Scene { WindowGroup { ContentView() } }
}
```

**Step 3 — log.**

```swift
AgentLogger.info("user logged in", metadata: ["userId": "42"])
AgentLogger.error("decoding failed", error: err)
AgentLogger.category("Network").debug("GET /users → 200")
```

**Step 4 — wire the endpoint into your build.**

Standard Xcode pattern: a Build Setting flows into `Info.plist` via `$(VAR)`
substitution. One-time project change, then every build (Xcode, xcodebuild,
TestFlight, CI) picks it up.

In your **Info.plist**, add once:

```xml
<key>AgentLoggerEndpoint</key>
<string>$(AGENTLOGGER_ENDPOINT)</string>

<key>NSLocalNetworkUsageDescription</key>
<string>Ships debug logs to your Mac during development.</string>

<key>NSAppTransportSecurity</key>
<dict><key>NSAllowsLocalNetworking</key><true/></dict>
```

Set a default in your Debug `.xcconfig` (or in target Build Settings):

```xcconfig
AGENTLOGGER_ENDPOINT = http://127.0.0.1:8765
```

Override per-build for real devices on the LAN:

```bash
MAC_IP="$(ipconfig getifaddr en0 || echo 127.0.0.1)"
xcodebuild build -scheme MyApp -workspace MyApp.xcworkspace -configuration Debug \
  -destination "platform=iOS,id=$DEVICE_UDID" \
  AGENTLOGGER_ENDPOINT="http://${MAC_IP}:8765"
```

Build-setting precedence is **CLI > xcconfig > project file**, so each
developer can override on their own machine without committing IPs to git.

For full coverage of every xcodebuild / simctl / devicectl flow, see
[`docs/integrating-xcodebuild.md`](docs/integrating-xcodebuild.md).

---

## CLI essentials

```bash
agentlog server status                                    # is the daemon up?
agentlog sessions latest --bundle <id> --json             # newest run
agentlog logs   --bundle <id> --level error --since 5m    # what blew up?
agentlog tail   --bundle <id>                             # live stream
agentlog search "<phrase>" --bundle <id>                  # FTS across history
agentlog db prune --before 7d                             # housekeeping
agentlog --help                                           # everything else
```

Append `--json` to any read command for NDJSON output (one record per line).

---

## Running the daemon

Three options, pick one:

| When | How |
|---|---|
| **Permanent** (recommended) | `agentlog server install` → registers an auto-start entry on this OS (launchd on macOS, Startup-folder bat on Windows). Restarts on login. |
| **Development** | `agentlog server start` (foreground; Ctrl-C to stop) or `make server-start` |
| **Background one-off** | `agentlog server start &` (Unix) / `start "" /B agentlog server start` (Windows) |

Common flags: `--port 8765`, `--bind 0.0.0.0` (real-device access), `--data-dir <path>`. Stop with `agentlog server stop`, uninstall with `agentlog server uninstall`.

**Cross-platform**: the daemon ships as a single static-linked binary for **macOS (universal)**, **Windows (amd64/arm64)**, and **Linux (amd64/arm64)**. `make release` produces all of them. Stop semantics: Unix uses SIGTERM, Windows uses a loopback-only HTTP shutdown endpoint — same `agentlog server stop` command on both.

Data lives in `~/.agentlogger/` (`agentlog.sqlite`, `agentlog.pid`, `server.log`).

---

## Agent integration

Bundled Claude Code skills under `.claude/skills/`:

- **`agentlogger-investigate`** — auto-triggers when the user reports a runtime symptom; pulls logs before guessing. Platform-agnostic.
- **`agentlogger-new-sdk`** — on-ramp for contributors adding SDKs for new languages.

After `./scripts/install.sh` they're copied into `~/.claude/skills/` for global availability.

---

## How it works

```
┌─────────────────────────────┐         ┌──────────────────────────────┐
│ App (sim / device / web)    │  HTTP   │ Mac (your dev machine)       │
│  AgentLogger SDK (zero-dep) │ ──────▶ │  agentlog server :8765       │
│  one-line bootstrap         │  batch  │   └ SQLite (WAL + FTS5)      │
└─────────────────────────────┘         │  agentlog tail / logs / …    │
                                        └──────────────────────────────┘
```

Endpoint resolution (in `.auto` mode): Info.plist `AgentLoggerEndpoint` → `AGENTLOGGER_ENDPOINT` env. No mDNS, no auth, no implicit localhost fallback — if neither is configured the SDK silently disables. Explicit by design.

---

## Project layout

```
server/         Go daemon + CLI (single binary, no CGO)
sdks/swift/     iOS / macOS SDK (Foundation only, iOS 13+)
demo/           SwiftUI sample app wired to the local SDK
docs/           Wire protocol + SDK behavior contracts
scripts/        install.sh
.claude/skills/ Agent skills (bundled)
```

---

## Adding a new SDK (Kotlin / JS / Dart / …)

Two contracts every SDK obeys:

- **[`docs/wire-protocol.md`](docs/wire-protocol.md)** — HTTP + JSON shapes, idempotency, status codes
- **[`docs/sdk-behavior.md`](docs/sdk-behavior.md)** — 14 behavior clauses (bootstrap, queue, backoff, discovery, …)

Open the repo in Claude Code and say *"add a Kotlin SDK"* — the bundled `agentlogger-new-sdk` skill turns the contracts into a step-by-step recipe with per-language patterns and a PR checklist. New SDKs go under `sdks/<lang>/`.

---

## Development

```bash
make doctor        # toolchain check
make build         # → bin/agentlog
make test          # Go + Swift unit tests
make test-e2e      # full end-to-end (server + SDK)
make demo-run      # build + install + launch demo on booted simulator
make help          # all targets
```

## License

TBD.
