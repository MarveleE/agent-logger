# Integrating AgentLogger with xcodebuild workflows

The SDK needs to know **one thing** to start: the daemon's URL. The
recommended way to provide it is to wire it through a **Build Setting**
and let Xcode substitute it into `Info.plist` at build time — exactly the
same pattern teams use for `DEBUG_API_BASE_URL` and other per-environment
config. This works identically for Xcode IDE, `xcodebuild`, `xcodebuild test`,
`devicectl`, TestFlight builds, and CI.

This document covers:

- [The one-time project setup](#one-time-setup) — placeholder + default
- [Per-build override](#per-build-override) — command-line / scripts
- [Real-device LAN script](#real-device-lan-script) — the canonical run
  script that detects your Mac's IP at build time
- [Auto-generated Info.plist (Xcode 14+)](#auto-generated-infoplist-xcode-14)
  — same idea, no Info.plist file needed
- [Why not a separate injection step](#why-not-a-separate-injection-step)

## One-time setup

### 1. Declare the placeholder in `Info.plist`

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

`$(AGENTLOGGER_ENDPOINT)` is substituted at build time from a Build Setting
of the same name. Anything you can put in a Build Setting can flow through
here — env-specific URLs, server tiers, etc.

### 2. Set a default

Either in your Debug `.xcconfig`:

```xcconfig
AGENTLOGGER_ENDPOINT = http://127.0.0.1:8765
```

Or directly in the target's Build Settings (User-Defined section).

For Release, leave it empty:

```xcconfig
AGENTLOGGER_ENDPOINT =
```

`AgentLoggerEndpoint` in Info.plist will resolve to `""` and the SDK skips
this layer entirely. Pair with `AgentLogger.bootstrap(.disabled)` in Release
for total silence.

If your project uses `xcodegen` / `tuist`, the same is just one block in
your `project.yml` / `Project.swift`. See
[`demo/AgentLoggerDemo/project.yml`](../demo/AgentLoggerDemo/project.yml)
for the canonical example.

### 3. Bootstrap

```swift
import AgentLogger

@main
struct MyApp: App {
    init() {
        #if DEBUG
        AgentLogger.bootstrap()        // reads $(AGENTLOGGER_ENDPOINT) from Info.plist
        #else
        AgentLogger.bootstrap(.disabled)
        #endif
    }
    var body: some Scene { WindowGroup { ContentView() } }
}
```

That's it. Every subsequent build — IDE, `xcodebuild`, CI, TestFlight —
carries the right endpoint without any extra wiring.

## Per-build override

Build-setting precedence is **command line > xcconfig > project file**.
Override at the xcodebuild call to point a particular build at a different
daemon — no project changes needed:

```bash
xcodebuild build \
  -workspace MyApp.xcworkspace -scheme MyApp -configuration Debug \
  -destination "platform=iOS Simulator,name=iPhone 16" \
  AGENTLOGGER_ENDPOINT="http://10.0.0.42:8765"
```

For `xcodebuild test`, exactly the same syntax — the test bundle inherits
the substituted Info.plist value:

```bash
xcodebuild test \
  -workspace MyApp.xcworkspace -scheme MyApp \
  -destination "platform=iOS Simulator,name=iPhone 16" \
  AGENTLOGGER_ENDPOINT="http://127.0.0.1:8765"
```

For Xcode IDE: set the build setting in target → Build Settings (or in your
`.xcconfig`). The IDE picks it up; no env vars on the scheme needed.

## Real-device LAN script

Real devices need the Mac's LAN address, which differs per developer.
Detect at build time and bake it in:

```bash
#!/usr/bin/env bash
set -euo pipefail

PORT=8765
DEVICE_UDID="${DEVICE_UDID:?set DEVICE_UDID to the target device}"
BUNDLE_ID="com.example.MyApp"

MAC_IP="$(ipconfig getifaddr en0 \
       || ipconfig getifaddr en1 \
       || echo 127.0.0.1)"
AGENTLOGGER_ENDPOINT="http://${MAC_IP}:${PORT}"

echo "[agentlog] daemon  :${PORT} on 0.0.0.0"
echo "[agentlog] device endpoint = ${AGENTLOGGER_ENDPOINT}"

agentlog server start --port "${PORT}" --bind 0.0.0.0 -q &
DAEMON_PID=$!
trap 'kill $DAEMON_PID 2>/dev/null || true' EXIT INT TERM

xcodebuild build \
  -workspace MyApp.xcworkspace -scheme MyApp -configuration Debug \
  -destination "platform=iOS,id=${DEVICE_UDID}" \
  -derivedDataPath .build/device \
  AGENTLOGGER_ENDPOINT="${AGENTLOGGER_ENDPOINT}" \
  | xcbeautify

APP_PATH=".build/device/Build/Products/Debug-iphoneos/MyApp.app"
xcrun devicectl device install app --device "${DEVICE_UDID}" "${APP_PATH}"
xcrun devicectl device process launch --device "${DEVICE_UDID}" "${BUNDLE_ID}"

agentlog tail --bundle "${BUNDLE_ID}"
```

Each developer runs this on their own Mac — no committed IPs, no shared
config to coordinate.

## Auto-generated Info.plist (Xcode 14+)

If your project uses Xcode's auto-generated Info.plist (no explicit
`Info.plist` file), use the `INFOPLIST_KEY_*` family of build settings.
Xcode prepends `INFOPLIST_KEY_` to the actual key name:

```xcconfig
// Debug.xcconfig
INFOPLIST_KEY_AgentLoggerEndpoint = http://127.0.0.1:8765
INFOPLIST_KEY_NSLocalNetworkUsageDescription = Ships debug logs to your Mac during development.
```

Override per-build:

```bash
xcodebuild build ... INFOPLIST_KEY_AgentLoggerEndpoint="http://10.0.0.42:8765"
```

For `NSAppTransportSecurity` you still need an explicit Info.plist snippet
in `INFOPLIST_FILE` because nested dicts can't go through `INFOPLIST_KEY_*`.

## Why not a separate injection step

Earlier versions of this repo shipped a `scripts/inject-info-plist.sh` that
ran `PlistBuddy` on the Info.plist after copy. That's been **removed**: the
`$(VAR)` substitution pattern is the standard Xcode mechanism, version-
controlled, and works for every launch flow (IDE / xcodebuild / devicectl /
test / CI). PlistBuddy fights Xcode's own pipeline and bypasses xcconfig
precedence, which makes per-config and per-developer overrides awkward.

If you really need to mutate Info.plist on a built `.app` bundle outside the
build pipeline (e.g. patching a pre-built artifact you can't rebuild), use
`/usr/libexec/PlistBuddy` directly:

```bash
/usr/libexec/PlistBuddy -c "Set :AgentLoggerEndpoint http://10.0.0.42:8765" \
  /path/to/SomeApp.app/Info.plist
```

But for normal app development, prefer the build setting approach above.
