# AgentLogger — Swift Client SDK

Zero-dependency Swift package that ships iOS and macOS app logs to a local
`agentlog` daemon over HTTP. Built for agent-driven workflows where Xcode's
Console isn't available (e.g. `xcrun simctl launch`, CI runs, headless
debugging).

## Requirements

- iOS 13 / macOS 10.15 / tvOS 13 / watchOS 6 or newer
- Swift 5.5+ / Xcode 13.2+ (Swift Concurrency is back-deployed via the
  language runtime; the SDK does **not** use any iOS 15+ APIs)
- A running `agentlog` daemon on the developer's Mac

The package depends only on `Foundation` and `Network.framework`.

## Installation

### Swift Package Manager

```swift
// Package.swift
dependencies: [
    .package(url: "https://github.com/<your-org>/agentlogger-swift.git", from: "0.1.0")
]
```

For local development against this monorepo, point at the local path:

```swift
.package(path: "../sdks/swift")
```

## Usage

```swift
import AgentLogger

// Call once at app startup (e.g. in your `@main` struct).
AgentLogger.bootstrap()

// Anywhere afterwards:
AgentLogger.trace("about to fetch users")
AgentLogger.info("user logged in", metadata: ["userId": "42"])
AgentLogger.warning("cache miss for \(key)")
AgentLogger.error("decoding failed", error: someError)
AgentLogger.critical("unrecoverable state")

// Named sub-logger (category tag for filtering)
let net = AgentLogger.category("Network")
net.info("GET /users")
```

### Configuration

`bootstrap` accepts an `AgentLoggerConfiguration`. The default is `.auto`:

| Mode | Behavior |
|---|---|
| `.auto` (default) | Resolve at runtime: Info.plist `AgentLoggerEndpoint` → `AGENTLOGGER_ENDPOINT` env var. If neither is set, transport is silently disabled (no implicit localhost fallback). |
| `.endpoint(URL)` | Use a specific endpoint |
| `.disabled` | No-op everywhere (recommended for release builds) |

```swift
#if DEBUG
AgentLogger.bootstrap(.auto)
#else
AgentLogger.bootstrap(.disabled)
#endif
```

Tunable knobs (rarely needed):

```swift
let cfg = AgentLoggerConfiguration(
    mode: .auto,
    queueCapacity: 2048,          // bounded; drops oldest on overflow
    batchSize: 64,                // entries per HTTP POST
    batchInterval: 0.25,          // seconds between flushes
    httpTimeout: 10,
    minimumLevel: .trace,
    defaultCategory: nil,
    sessionMetadata: ["env": "dev"],
    discoveryTimeout: 3
)
AgentLogger.bootstrap(cfg)
```

### Behavior under failure

- `bootstrap()` is **idempotent** and **never throws** — calling it twice does nothing.
- Log calls are O(1) and synchronous — they enqueue and return immediately.
- The queue is bounded (default 2048 entries). When full, the oldest entries
  are dropped and the count is reported as `_dropped: N` metadata on the next
  successful batch.
- If the server is unreachable, transport is silently disabled and re-attempted
  every few seconds (exponential backoff: 1s → 2s → 4s → … → 30s).
- All errors are swallowed inside the SDK — the logger will never crash your
  app or surface exceptions to the call site.

### Identifying your app's session from an agent

Each launch creates a session keyed by `(bundleId, deviceId)`. From the CLI:

```bash
agentlog sessions latest --bundle com.example.MyApp --json
agentlog tail --bundle com.example.MyApp
```

The SDK picks up simulator metadata automatically from `ProcessInfo`:
`SIMULATOR_UDID`, `SIMULATOR_DEVICE_NAME`, etc.

## Real-device setup

For physical devices, the cleanest path is **build-time injection via
xcodebuild** — the endpoint flows through a Build Setting and lands in
Info.plist via `$(VAR)` substitution. This is the standard Xcode pattern,
identical to how teams ship `DEBUG_API_BASE_URL` or any other env-specific
config.

**Step 1 — declare the placeholder in Info.plist** (once):

```xml
<key>AgentLoggerEndpoint</key>
<string>$(AGENTLOGGER_ENDPOINT)</string>

<key>NSLocalNetworkUsageDescription</key>
<string>Ships debug logs to your Mac during development.</string>

<key>NSAppTransportSecurity</key>
<dict>
    <key>NSAllowsLocalNetworking</key>
    <true/>
</dict>
```

**Step 2 — default the build setting** in your `Debug.xcconfig` (or in
project settings):

```xcconfig
AGENTLOGGER_ENDPOINT = http://127.0.0.1:8765
```

For Release, leave the value empty so the Info.plist key resolves to `""`
and the SDK skips it (combine with `bootstrap(.disabled)`).

**Step 3 — override per-build** for real devices (the daemon must be
reachable, so use the Mac's LAN address):

```bash
MAC_IP="$(ipconfig getifaddr en0 || echo 127.0.0.1)"

xcodebuild build \
    -workspace MyApp.xcworkspace -scheme MyApp \
    -configuration Debug \
    -destination "platform=iOS,id=$DEVICE_UDID" \
    AGENTLOGGER_ENDPOINT="http://${MAC_IP}:8765"

xcrun devicectl device install app --device "$DEVICE_UDID" .../MyApp.app
xcrun devicectl device process launch --device "$DEVICE_UDID" com.example.MyApp
```

Xcode build-setting precedence is **command-line > xcconfig > project file**,
so each developer's machine can override without committing IPs to git.

## API reference

All log methods accept `metadata: [String: String]?` plus the standard
`#file` / `#line` / `#function` source-location parameters (auto-captured).

| Method | Notes |
|---|---|
| `AgentLogger.trace(_:)` | Most verbose |
| `AgentLogger.debug(_:)` | |
| `AgentLogger.info(_:)` | |
| `AgentLogger.notice(_:)` | |
| `AgentLogger.warning(_:)` | |
| `AgentLogger.error(_:, error:)` | `error` is auto-flattened into metadata |
| `AgentLogger.critical(_:)` | |
| `AgentLogger.category(_:)` | Returns a `CategoryLogger` |
| `AgentLogger.currentSessionID` | Active session UUID (or `nil`) |
| `AgentLogger.currentSession` | Full `SessionDescriptor` |
| `AgentLogger.shutdown()` | `async`, flushes and tears down |

## Testing

```bash
swift test                                      # unit tests
AGENTLOGGER_E2E_ENDPOINT=http://127.0.0.1:8765 swift test   # + E2E
```

The repo's `Makefile` wraps these as `make test-client` and the orchestrated
end-to-end harness as `make test-e2e`.
