---
name: agentlogger-integrate
description: When the user wants to wire AgentLogger into an existing iOS / macOS / multi-platform Swift project — phrases like "integrate AgentLogger / agentlog into this project", "add the agentlog SDK", "wire up log shipping to my Mac", "connect this app to the agentlog daemon", "set up agentlogger in my Xcode project", "I want to debug this app via terminal logs" — this skill walks through the four mandatory changes end-to-end. Engage even when the user just shares an iOS project and mentions agentlog without spelling out the steps. Supports projects driven by xcodegen (`project.yml`) or tuist (`Project.swift`); raw `.xcodeproj` projects get the deterministic edits done programmatically and explicit Xcode UI steps for the SPM dependency. Stop at the verification step — let the user actually run the daemon and re-launch the app to confirm.
---

# Integrate AgentLogger into an existing project

The integration boils down to **four changes**. Each is small, but skipping any one of them leaves the SDK silent at runtime:

1. **Add the Swift package dependency** so the app can `import AgentLogger`
2. **Wire `$(AGENTLOGGER_ENDPOINT)` into Info.plist** so the SDK knows which daemon to ship to
3. **Provide a default for the build setting** so Debug builds Just Work
4. **Insert `AgentLogger.bootstrap()`** at the app entry point so the SDK actually starts

The trick is doing all four without overlooking the third (silent failure: SDK starts, queues logs, but `AgentLoggerEndpoint` resolves to an empty string and the SDK disables itself). The instructions below cover the variants that show up in real projects.

## When to engage

Trigger when the user gives any of the following:

- "integrate / set up / add AgentLogger into this project"
- "wire up the agentlog SDK so I can see logs from xcodebuild runs"
- Drops a `project.yml` / `Project.swift` / `.xcodeproj` and says "make this ship logs to agentlog"
- Asks about "agentlog SDK install" or "connect this app to the daemon"
- Mentions `AgentLogger.bootstrap` and the call site is missing

Do **not** engage when:

- The user is debugging an existing already-integrated app → `agentlogger-investigate` is the right skill
- The user is *building a new SDK* for a different language → `agentlogger-new-sdk`
- They're calling existing SDK methods (`AgentLogger.info(...)`) — that's already integrated, the question is something else

## Step 0 — Detect the project flavor

Decide which path to take by looking at the project root in priority order:

1. `project.yml` exists → **xcodegen** path
2. `Project.swift` exists → **tuist** path
3. Only `.xcodeproj` directory (no spec file) → **raw Xcode** path

If the project uses both an xcodegen spec **and** a committed `.xcodeproj`, the spec is the source of truth — edit `project.yml` and regenerate. Same for tuist.

If multiple targets exist, ask the user which target to integrate (usually the main app target, not test bundles or extensions).

## Step 1 — Add the Swift package dependency

The package lives at `https://github.com/<your-org>/agentlogger-swift.git` (public release) or a local path during development.

### xcodegen (`project.yml`)

Add to the top-level `packages` block, and to the target's `dependencies`:

```yaml
packages:
  AgentLogger:
    url: https://github.com/<your-org>/agentlogger-swift.git
    from: 0.1.0
    # Or for local dev:
    # path: ../path/to/agentlogger/sdks/swift

targets:
  YourAppTarget:
    dependencies:
      - package: AgentLogger
        product: AgentLogger
```

Then regenerate the Xcode project:

```bash
xcodegen generate
```

### tuist (`Project.swift`)

Two parts. First, in `Tuist/Package.swift` (the tuist SPM manifest), add the dependency:

```swift
// Tuist/Package.swift
import PackageDescription

let package = Package(
    name: "PackageName",
    dependencies: [
        .package(url: "https://github.com/<your-org>/agentlogger-swift.git", from: "0.1.0"),
    ]
)
```

Then in `Project.swift`, add `.external(name: "AgentLogger")` to the target's dependencies:

```swift
let project = Project(
    name: "YourApp",
    targets: [
        .target(
            name: "YourApp",
            dependencies: [
                .external(name: "AgentLogger"),
            ],
            // ...
        ),
    ]
)
```

Then regenerate:

```bash
tuist install      # if dependencies changed
tuist generate
```

### Raw `.xcodeproj`

Editing `project.pbxproj` programmatically is fragile. Tell the user to add the package via Xcode UI:

1. Open the project in Xcode (`xed .` from the project root)
2. **File → Add Package Dependencies…**
3. Paste the package URL or click **Add Local…** to pick the on-disk path
4. Pick the app target when prompted

Wait for the user to confirm this is done before continuing. The rest of the steps (Info.plist, build setting, bootstrap call) you can still do programmatically.

## Step 2 — Wire Info.plist + build setting

The SDK reads `AgentLoggerEndpoint` from Info.plist at runtime. Use `$(VAR)` substitution so the value comes from a Build Setting — this is the standard Xcode pattern, allows per-config overrides via xcconfig, and is what `xcodebuild build AGENTLOGGER_ENDPOINT=…` operates on.

The three Info.plist keys that must be present:

| Key | Value | Why |
|---|---|---|
| `AgentLoggerEndpoint` | `$(AGENTLOGGER_ENDPOINT)` | Resolved at build time from the build setting |
| `NSLocalNetworkUsageDescription` | something user-visible | Required by iOS 14+ to talk to a non-loopback host |
| `NSAppTransportSecurity.NSAllowsLocalNetworking` | `true` | Allows http:// on the local network |

### The single LAN mental model

**Both the iOS Simulator and a real device on the same WiFi are network clients reaching the Mac.** Use the **Mac's LAN IP** for both — there is no useful distinction. The `http://127.0.0.1:8765` value some older guides show only happens to work for the simulator (which shares the host's loopback) and silently breaks the moment you try a real device.

Detect the Mac's LAN IP at build time:

```bash
ipconfig getifaddr en0 || ipconfig getifaddr en1
# → e.g. 192.168.1.42
```

Pass it as a build setting at the xcodebuild CLI — same shape for every flow (Xcode `Run`, `xcodebuild test`, `xcrun devicectl`):

```bash
MAC_IP="$(ipconfig getifaddr en0 || ipconfig getifaddr en1)"
xcodebuild build ... AGENTLOGGER_ENDPOINT="http://${MAC_IP}:8765"
```

The xcconfig default can stay empty (forces CLI to provide), or you can hardcode the current LAN IP per-developer in a gitignored `Local.xcconfig` to make Xcode IDE Run work without flags:

```xcconfig
// Local.xcconfig (gitignore'd, each developer fills in their own machine)
AGENTLOGGER_ENDPOINT = http://192.168.1.42:8765
```

```xcconfig
// Release.xcconfig (committed)
AGENTLOGGER_ENDPOINT =     // empty → AgentLoggerEndpoint resolves to "" → SDK skips
```

The IP changes when DHCP reassigns. If that's a frequent issue for the user, suggest either a DHCP reservation on their router or a small shell script that updates `Local.xcconfig` from `ipconfig getifaddr` on each build.

The daemon must accept LAN connections — that's the default (`agentlog start` binds to `0.0.0.0`). If the user explicitly restricted to loopback via `--bind 127.0.0.1`, the LAN endpoint won't reach it.

### xcodegen

Add to the target block in `project.yml`:

```yaml
targets:
  YourAppTarget:
    info:
      path: Resources/Info.plist          # or wherever the project's plist lives
      properties:
        AgentLoggerEndpoint: $(AGENTLOGGER_ENDPOINT)
        NSLocalNetworkUsageDescription: Ships debug logs to your Mac during development.
        NSAppTransportSecurity:
          NSAllowsLocalNetworking: true
        # ... keep the existing keys here ...
    settings:
      configs:
        Debug:
          AGENTLOGGER_ENDPOINT: http://127.0.0.1:8765
        Release:
          AGENTLOGGER_ENDPOINT: ""
```

If the project uses an explicit Info.plist file (not auto-generated), open it and add the three keys directly. xcodegen's `properties` block in this case just supplements the file.

Regenerate: `xcodegen generate`.

### tuist

In `Project.swift`, extend the target's `infoPlist` and add `settings`:

```swift
.target(
    name: "YourApp",
    infoPlist: .extendingDefault(with: [
        "AgentLoggerEndpoint": "$(AGENTLOGGER_ENDPOINT)",
        "NSLocalNetworkUsageDescription": "Ships debug logs to your Mac during development.",
        "NSAppTransportSecurity": [
            "NSAllowsLocalNetworking": true,
        ],
    ]),
    settings: .settings(
        configurations: [
            .debug(name: "Debug", settings: [
                "AGENTLOGGER_ENDPOINT": "http://127.0.0.1:8765",
            ]),
            .release(name: "Release", settings: [
                "AGENTLOGGER_ENDPOINT": "",
            ]),
        ]
    ),
    dependencies: [
        .external(name: "AgentLogger"),
    ]
)
```

If the project uses a checked-in `Info.plist`, edit it and add the three keys with `/usr/libexec/PlistBuddy`:

```bash
PLIST="Resources/Info.plist"
PB=/usr/libexec/PlistBuddy
$PB -c "Add :AgentLoggerEndpoint string \$(AGENTLOGGER_ENDPOINT)" "$PLIST"
$PB -c "Add :NSLocalNetworkUsageDescription string 'Ships debug logs to your Mac during development.'" "$PLIST"
$PB -c "Add :NSAppTransportSecurity dict" "$PLIST"
$PB -c "Add :NSAppTransportSecurity:NSAllowsLocalNetworking bool true" "$PLIST"
```

Regenerate: `tuist generate`.

### Raw `.xcodeproj`

Edit `Info.plist` with PlistBuddy as above. For the build setting, two options:

- **Option A — xcconfig** (recommended, version-controlled). Find the project's `.xcconfig` files (usually `Debug.xcconfig` / `Release.xcconfig`). Append:

  ```
  // Debug.xcconfig
  AGENTLOGGER_ENDPOINT = http://127.0.0.1:8765
  ```
  And to Release.xcconfig:
  ```
  AGENTLOGGER_ENDPOINT =
  ```

  If no xcconfig exists, tell the user to create one and attach it via **Project → Info → Configurations**.

- **Option B — Build Settings UI**. Open the project in Xcode, select the target, **Build Settings**, click **+ → Add User-Defined Setting**, name `AGENTLOGGER_ENDPOINT`, set Debug = `http://127.0.0.1:8765`, Release = empty.

Either works. Prefer A if you can.

## Step 3 — Insert `AgentLogger.bootstrap()` at the app entry point

The SDK does nothing until `bootstrap` is called. Find the right call site for the project's lifecycle:

### SwiftUI App (modern, iOS 14+ / macOS 11+)

Locate the file with `@main` and the `App` conformance. Insert `import AgentLogger` at the top and call `bootstrap()` in the struct's `init()`:

```swift
import SwiftUI
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

    var body: some Scene {
        WindowGroup { ContentView() }
    }
}
```

The `#if DEBUG` / `.disabled` guard makes Release builds a strict no-op (no queue allocation, no background task, no log calls evaluated).

### UIKit AppDelegate (legacy, including SwiftUI apps that use `UIApplicationDelegateAdaptor`)

Add the import and call inside `application(_:didFinishLaunchingWithOptions:)`, before `return true`:

```swift
import UIKit
import AgentLogger

@UIApplicationMain
class AppDelegate: UIResponder, UIApplicationDelegate {
    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
        #if DEBUG
        AgentLogger.bootstrap()
        #else
        AgentLogger.bootstrap(.disabled)
        #endif
        return true
    }
}
```

### macOS NSApplicationDelegate

Same as UIKit, but `applicationDidFinishLaunching(_:)`:

```swift
import Cocoa
import AgentLogger

@NSApplicationMain
class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        #if DEBUG
        AgentLogger.bootstrap()
        #else
        AgentLogger.bootstrap(.disabled)
        #endif
    }
}
```

### Other entry points

For unusual cases (Swift `@main` on a command-line tool, custom main, multi-target apps with shared init), bootstrap in whichever code path runs first when the app launches. The contract is just: `bootstrap` must run before any `AgentLogger.info/...` call you care about.

`bootstrap` is idempotent — calling it twice does nothing the second time. So if there's any chance of double-init, that's fine.

## Step 4 — Verify

Don't declare it done from a clean compile. Confirm the SDK actually shipped a log line. Same command shape for simulator and real device — only `-destination` and the install/launch tool differ:

```bash
# Start the daemon (separate terminal). Bound to 0.0.0.0 by default.
agentlog dev

# In another terminal, detect the Mac's address and bake it into the build.
MAC_IP="$(ipconfig getifaddr en0 || echo 127.0.0.1)"

# --- For an iOS Simulator: ---
xcodebuild build -scheme YourApp -configuration Debug \
  -destination 'generic/platform=iOS Simulator' \
  -derivedDataPath .build \
  AGENTLOGGER_ENDPOINT="http://${MAC_IP}:8765"
xcrun simctl install booted .build/Build/Products/Debug-iphonesimulator/YourApp.app
xcrun simctl launch --terminate-running-process booted com.example.YourApp

# --- For a real device: ---
xcodebuild build -scheme YourApp -configuration Debug \
  -destination "platform=iOS,id=$DEVICE_UDID" \
  -derivedDataPath .build \
  AGENTLOGGER_ENDPOINT="http://${MAC_IP}:8765"
xcrun devicectl device install app --device "$DEVICE_UDID" \
  .build/Build/Products/Debug-iphoneos/YourApp.app
xcrun devicectl device process launch --device "$DEVICE_UDID" com.example.YourApp

# Expect either one: a log appears in the dev terminal.
# If silence:
agentlog sessions list --bundle com.example.YourApp
#   → empty: bootstrap didn't run, or AgentLoggerEndpoint resolved to ""
#   → returns a session: SDK started but no logs yet — add an AgentLogger.info() somewhere
```

## Common pitfalls

- **Forgetting Step 2's xcconfig default**: Info.plist gets `<string>$(AGENTLOGGER_ENDPOINT)</string>` but the build setting is never declared anywhere. At build time it resolves to the literal string `$(AGENTLOGGER_ENDPOINT)` (Xcode doesn't error on undefined). The SDK reads that, tries `URL(string:)`, gets nil, disables itself silently. Always check the built `.app/Info.plist` actually contains a resolved URL: `/usr/libexec/PlistBuddy -c "Print :AgentLoggerEndpoint" path/to/built.app/Info.plist`.
- **Plist Buddy on YAML-driven projects**: if xcodegen or tuist generates the Info.plist from spec, edits via PlistBuddy get overwritten on the next `xcodegen generate` / `tuist generate`. Put the keys in the spec instead.
- **Using `127.0.0.1` at all**: it only happens to work for simulator (which shares the host's loopback) and silently breaks the moment you switch to a real device. Use the Mac's LAN IP from the start — same value works for both targets (see Step 2).
- **Daemon bound to 127.0.0.1**: if the user explicitly ran `agentlog start --bind 127.0.0.1`, the LAN endpoint won't reach it. Default is `0.0.0.0` — confirm with `agentlog status` or check the startup log.
- **Missing `NSLocalNetworkUsageDescription` on iOS 14+**: app crashes or the OS rejects the network call. Always include this key.
- **`bootstrap()` inside a `lazy var` or computed property**: it runs on first access, not at launch. Put it directly in `init()` / `application(_:didFinishLaunchingWithOptions:)`.
- **Multiple bootstrap calls in different SwiftUI Scenes**: harmless (idempotent), but confusing if someone reads the code expecting one canonical site. Pick one.

## Pointer back

For the full context on why each piece is structured this way (xcodebuild precedence rules, alternate INFOPLIST_KEY_* mechanism for auto-generated plists, the real-device LAN script pattern, the per-build override flow), see `docs/integrating-xcodebuild.md` in the AgentLogger repo. This skill is the **action recipe**; that doc is the **explanation**.
