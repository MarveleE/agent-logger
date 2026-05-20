# AgentLoggerDemo

SwiftUI sample app that exercises the AgentLogger SDK against a local
`agentlog` daemon. Lives in this repo so we can validate the SDK end-to-end
through real `xcodebuild` flows.

## Quick start (one-liner)

```bash
# from the repo root
make server-start &        # in one terminal
make demo-run              # in another
```

`make demo-run` builds the app, **bakes the daemon URL into Info.plist via the
`$(AGENTLOGGER_ENDPOINT)` build setting**, installs it on the booted
simulator, and launches it. Logs appear in
`agentlog tail --bundle com.agentlogger.demo`.

## How endpoint injection works

The demo is a reference implementation of the recommended Xcode pattern:

1. `project.yml` declares `AgentLoggerEndpoint: $(AGENTLOGGER_ENDPOINT)` in
   the Info.plist properties.
2. `project.yml` sets a Debug default of `http://127.0.0.1:8765` via a build
   setting (and `""` for Release so the SDK silently disables).
3. xcodegen materializes that into the Xcode project + Info.plist.
4. `xcodebuild` substitutes `$(AGENTLOGGER_ENDPOINT)` at build time. The
   built `.app/Info.plist` contains the resolved URL.
5. The SDK reads `AgentLoggerEndpoint` from Info.plist at bootstrap — no
   environment variables, no SIMCTL_CHILD wiring.

To target a different endpoint (e.g. a real device on LAN), pass the build
setting on the command line:

```bash
xcodebuild build -project AgentLoggerDemo.xcodeproj -scheme AgentLoggerDemo \
  -destination 'generic/platform=iOS Simulator' -configuration Debug \
  -derivedDataPath .build \
  AGENTLOGGER_ENDPOINT="http://10.0.0.42:8765"
```

Build-setting precedence: **CLI > xcconfig > project file**.

## What it does

`AgentLoggerDemoApp.swift` calls `AgentLogger.bootstrap()` once at startup,
then emits an `info` log. `ContentView` provides buttons for each severity
level, plus a burst test that ships N entries to validate batching and the
overflow drop counter.

## Project layout

```
AgentLoggerDemo/
├── project.yml                  # xcodegen spec (source of truth)
├── AgentLoggerDemo.xcodeproj/   # generated; commit so non-xcodegen users can open
└── AgentLoggerDemo/
    ├── AgentLoggerDemoApp.swift # @main, bootstrap + initial log
    ├── ContentView.swift        # SwiftUI buttons / burst test
    └── Info.plist               # generated; do not edit
```

The SDK is referenced as a local Swift package at `../../sdks/swift`.

## Manually building & running

```bash
# build (endpoint defaults to the Debug xcconfig value)
xcodebuild build -project AgentLoggerDemo.xcodeproj -scheme AgentLoggerDemo \
                 -destination 'generic/platform=iOS Simulator' \
                 -configuration Debug -derivedDataPath .build

# install on the booted simulator
xcrun simctl install booted .build/Build/Products/Debug-iphonesimulator/AgentLoggerDemo.app

# launch — endpoint already in the app, no env vars needed
xcrun simctl launch booted com.agentlogger.demo
```

## Regenerating the project

```bash
make demo-generate           # runs `xcodegen generate` under demo/AgentLoggerDemo
```

## Caveats

- Demo's minimum is iOS 14 because SwiftUI's `App` lifecycle is iOS 14+.
  The AgentLogger SDK itself supports iOS 13. If you need iOS 13 support in
  your own app, use `UIApplicationDelegateAdaptor` + `UIHostingController`.
- The Xcode project is unsigned (`CODE_SIGNING_ALLOWED=NO`) — it only builds
  for the simulator. To run on a real device, add a development team in
  `project.yml` and regenerate.

For the full guide on every xcodebuild flow, see
[`docs/integrating-xcodebuild.md`](../../docs/integrating-xcodebuild.md).
