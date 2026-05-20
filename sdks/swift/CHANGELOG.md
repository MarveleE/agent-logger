# Changelog

All notable changes to the AgentLogger Swift SDK.

## Unreleased

### Added
- Initial v1 SDK
  - Zero-dependency Swift package (Foundation + Network.framework only)
  - iOS 13 / macOS 10.15 / tvOS 13 / watchOS 6 minimums
  - `AgentLogger.bootstrap` with auto / endpoint / disabled modes
  - Synchronous, non-blocking log API at all severity levels
  - Bounded async queue with drop-oldest semantics + drop counter
  - Batched HTTP transport (`dataTask` + `withCheckedThrowingContinuation`)
  - Automatic session metadata capture (bundle, device, simulator env)
  - `CategoryLogger` for tagged sub-loggers
  - Exponential backoff with 30s cap
  - Lifecycle hooks (UIKit foreground/background) on iOS
  - Endpoint resolution precedence: Info.plist `AgentLoggerEndpoint`
    → `AGENTLOGGER_ENDPOINT` env. No implicit localhost fallback — if
    neither is set the SDK silently disables transport.
  - `SessionDescriptor.platform` — compile-time runtime tag (`ios` / `macos`
    / `tvos` / `watchos` / `linux` / ...). Wire field added in
    wire-protocol v1 as a backward-compatible optional.
  - `SessionDescriptor.appBuild` — `CFBundleVersion` distinct from
    `appVersion` (the semver-style `CFBundleShortVersionString`).
  - `deviceKind` vocabulary expanded to an open enum (`simulator | emulator
    | device | browser | desktop | server | container | embedded`).

### Planned for next release
- Optional `AgentLoggerSwiftLog` adapter as a separate package
