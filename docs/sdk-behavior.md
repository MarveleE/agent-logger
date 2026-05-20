# AgentLogger SDK Behavior Spec

The [wire protocol](./wire-protocol.md) defines what an SDK puts on the network. This document defines how it must **behave**. Together they're the full contract; the reference Swift SDK is the gold-standard implementation but not the source of truth — this document is.

A conforming SDK is one whose externally observable behavior matches every clause below, regardless of language idioms used internally.

> **Conventions in this document**: **MUST**, **SHOULD**, **MAY** carry their usual RFC-2119 weight. "Reference SDK" means the Swift package in `sdks/swift/`.

---

## 1. Bootstrap

- **1.1** An SDK MUST expose a single entry point — the equivalent of `AgentLogger.bootstrap(config)` — that configures the logger and starts background work.
- **1.2** `bootstrap` MUST be idempotent. The first call wins; subsequent calls return immediately without altering state and without throwing. This is so application code can safely call it from multiple init paths.
- **1.3** `bootstrap` MUST be **non-blocking**. It MUST NOT do any synchronous network I/O. Discovery, session registration, and the sender loop all happen on a background task started here.
- **1.4** `bootstrap` MUST accept a configuration that includes at minimum:
  - mode: `auto` / `endpoint(URL)` / `disabled`
  - queue capacity (default 2048)
  - batch size (default 64)
  - batch interval (default 250 ms)
  - HTTP timeout (default 10 s)
  - minimum level (default trace = log everything)
  - additional session metadata (free-form map<string,string>)
- **1.5** `bootstrap` MUST gather session metadata (`bundleId`, `deviceId`, `deviceName`, `deviceKind`, `platform`, `osVersion`, `appVersion`, `appBuild`) **synchronously** so the session UUID and metadata are available immediately to callers. SDKs collect these from whatever the local platform exposes — see the table below for canonical sources. Fields the platform cannot provide MAY be omitted (sent as absent in the JSON). The `platform` value SHOULD be set unconditionally to whichever of the recommended runtime tags applies (`ios`, `android`, `web`, `node`, …); it's the most useful filter for cross-platform queries.

### Per-platform collection table (informational)

For new SDK implementers — the recommended source of each field. Treat this as the default; SDKs MAY substitute equivalents.

| Field | iOS / macOS | Android | Web (browser) | Node / CLI | Flutter |
|---|---|---|---|---|---|
| `bundleId` | `Bundle.main.bundleIdentifier` | `applicationContext.packageName` | user-supplied via config; default `location.host` | `process.env.npm_package_name` or config | per-platform native call |
| `deviceId` | `UIDevice.identifierForVendor.uuidString` | `Settings.Secure.ANDROID_ID` | persisted `localStorage` UUID (synthetic) | `os.hostname()` + persisted UUID | per-platform native call |
| `deviceName` | `UIDevice.current.name` (iOS) / `Host.current().localizedName` (macOS) | `Build.MODEL` | parsed `navigator.userAgent` | `os.hostname()` | per-platform native call |
| `deviceKind` | `SIMULATOR_UDID` in env → `simulator`, else `device` | `Build.FINGERPRINT` heuristic → `emulator` / `device` | always `browser` | `server` | per-platform |
| `platform` | compile-time `os(iOS)` → `ios`, `os(macOS)` → `macos`, etc. | `android` | `web` | `node` | per-platform |
| `osVersion` | `UIDevice.systemVersion` (iOS) / `ProcessInfo.operatingSystemVersion` (macOS) | `Build.VERSION.RELEASE` | UA-derived (browser + version) | `os.release()` | per-platform |
| `appVersion` | `CFBundleShortVersionString` | `PackageInfo.versionName` | build-time `process.env.APP_VERSION` | `package.json#version` | `package_info_plus` |
| `appBuild` | `CFBundleVersion` | `PackageInfo.versionCode` | git short SHA / CI build id | git short SHA | per-platform |

## 2. Public log API

- **2.1** An SDK MUST expose, at minimum, log methods for each of the seven levels: `trace`, `debug`, `info`, `notice`, `warning`, `error`, `critical`.
- **2.2** Each method MUST accept at minimum: a message (string, optionally lazy/by-name to skip formatting when filtered), and optional metadata (map<string,string>).
- **2.3** Each method MUST be **synchronous from the caller's perspective and O(1)**: it enqueues and returns. It MUST NOT perform network I/O on the calling thread, MUST NOT block on a lock for longer than a typical contended mutex acquisition, and MUST NOT throw / propagate errors.
- **2.4** SDKs SHOULD support an `error()` overload that takes an exception/error object and auto-flattens at least its type and message into `metadata` (e.g. `errorType`, `errorDescription`).
- **2.5** SDKs SHOULD expose a `category(name)` factory that returns a sub-logger tagging entries with that category. Category propagates to all level methods on the sub-logger.
- **2.6** SDKs MAY capture source location (`file`, `line`, `func`) automatically using whatever the language offers (`#file`/`#line`/`#function`, stack inspection, build-time macros). Where possible this SHOULD be opt-out, not opt-in.

## 3. Queue

- **3.1** An SDK MUST maintain a bounded, in-memory FIFO queue of log entries between the public log API and the HTTP sender.
- **3.2** Capacity is the configured `queueCapacity` (default 2048 entries).
- **3.3** When the queue is full, the SDK MUST **drop the oldest** entry to make room for the newest, and MUST increment a "dropped" counter.
- **3.4** The dropped counter MUST be piggybacked on the next successful batch: the first entry of that batch has `metadata._dropped` set to the count, and the counter resets to zero. This is the **only** mechanism by which dropped events are surfaced — SDKs MUST NOT log them as new entries or expose them through the public API.
- **3.5** Enqueue MUST be safe to call from any thread / async context and MUST NOT block.

## 4. Batching and the sender loop

- **4.1** A single background task drains the queue and POSTs `/v1/sessions/{id}/logs`.
- **4.2** A batch is shipped when **either** of these conditions is met:
  - `batchSize` entries are pending (default 64), **or**
  - `batchInterval` has elapsed since the last flush (default 250 ms) and the queue has ≥ 1 entry.
- **4.3** Each entry's `seq` MUST be a monotonically increasing 64-bit integer per session, starting at 1 and incrementing by 1 in the order log calls were observed.
- **4.4** A batch MUST contain at most 1024 entries (the server's limit).
- **4.5** On a successful `2xx` response, the SDK MUST clear those entries from its retry buffer and reset the backoff counter.
- **4.6** On any failure (network error, 5xx), the SDK MUST keep the batch and retry after the backoff delay. Entries are not re-enqueued in front of new ones — they stay first in line. The simplest implementation is to `prepend` the failed batch back onto the internal sender buffer.
- **4.7** A `4xx` response (other than 404, see § 6) indicates a client bug and MUST be logged to a platform-appropriate channel (stderr, OSLog, Logcat) **once** and treated as a non-retryable success — drop the batch so a permanent malformed entry doesn't poison the queue.

## 5. Exponential backoff

- **5.1** Backoff sequence: `1, 2, 4, 8, 16, 30, 30, 30, …` seconds. SDKs MUST cap at 30 seconds.
- **5.2** Any successful HTTP exchange resets the counter to zero.
- **5.3** Backoff applies to both discovery probes and ingest. Failed discovery loops the same curve.
- **5.4** During backoff, the public log API continues to accept entries (subject to § 3 drop rules). The user never sees a "logger blocked" effect.

## 6. Session lifecycle

- **6.1** The SDK generates a session ID at bootstrap (UUID recommended) and uses it for the lifetime of the process.
- **6.2** Before sending any log batch, the SDK MUST successfully `POST /v1/sessions` (with its generated id). The sender loop registers, then drains.
- **6.3** A 404 on `/v1/sessions/{id}/logs` means the server's storage was reset or the session expired (future feature). SDKs MUST re-register the session (re-POST `/v1/sessions` with the same id) and continue. They MUST NOT generate a new id silently — that would split the user's logs across "phantom" sessions.
- **6.4** SDKs MAY optionally send `PUT /v1/sessions/{id}/heartbeat` when idle. They MUST NOT send it more often than once every 30 seconds. v1 reference SDKs do not implement heartbeats.
- **6.5** The session id MUST be readable via a public read-only property (`currentSessionID` or equivalent) so the host app can correlate logs externally.

## 7. Endpoint discovery (mode: `.auto`)

Two explicit sources, no implicit defaults. The user must configure at least one for `.auto` to resolve to anything.

Priority (highest first):

1. **Pinned config baked into the app bundle.** Platform-specific:
   - iOS / macOS: `Info.plist` key `AgentLoggerEndpoint`
   - Android: `<meta-data android:name="AgentLoggerEndpoint" android:value="…"/>` in `AndroidManifest.xml`
   - Web: `<meta name="agentlogger-endpoint" content="…">` or `window.AGENTLOGGER_ENDPOINT`
   - Flutter: `String.fromEnvironment('AGENTLOGGER_ENDPOINT', defaultValue: '')` baked at build time, or a `--dart-define`
   - Server-side / Node / CLI hosts: typically not applicable; require explicit `.endpoint(URL)` at bootstrap
2. **Environment variable** `AGENTLOGGER_ENDPOINT`. iOS simulators receive this via `SIMCTL_CHILD_*`. Android emulators via `BuildConfig` or a process env. Other platforms via whatever their native env mechanism is.
3. If neither resolves, transport is **disabled** until the next retry. Logs continue to enqueue (and drop on overflow) so the host app is never affected. This is the conservative default — SDKs MUST NOT silently fall back to `http://127.0.0.1`, `http://10.0.2.2`, or any other "well-known" address. Users opt in by setting one of the two sources above.

For each candidate, the SDK MUST verify reachability via `GET /v1/health` (≤ 3 s timeout) before adopting it. The first candidate that responds 200 wins.

After a transport failure (§ 4.6), SDKs SHOULD re-run discovery before the next attempt — the user may have started the server in the meantime.

## 8. Lifecycle / flush

- **8.1** SDKs MUST hook into the host platform's "going to background / hidden / suspending" event and trigger an immediate flush of pending entries. Best effort — if the platform kills the process before the flush completes, the loss is acceptable.
- **8.2** SDKs MUST NOT hook process termination signals in a way that delays shutdown noticeably.
- **8.3** SDKs SHOULD expose a manual `shutdown()` that:
  - stops accepting new entries (or accepts and drops them — implementation choice)
  - drains the queue with a configurable deadline (default 2 s)
  - tears down the sender task
  - is idempotent

## 9. Disabled mode

When configured with `.disabled` (typically release builds):

- **9.1** No network I/O, no background tasks, no session.
- **9.2** Public log methods are no-ops with O(1) cost. No allocation of the message string SHOULD happen when the message is given lazily.
- **9.3** `currentSessionID` returns null/nil.

## 10. Behavioral observability

SDKs SHOULD log internal errors (failed discovery, malformed config, repeated 5xx) to the platform's native log channel (stderr / OSLog / Logcat / browser console) with a clear `agentlogger:` prefix. They MUST NOT route their own internal errors through their own queue.

Rate limit: at most one internal error message per minute per error kind.

## 11. Threading guarantees

- **11.1** All public methods are safe to call from any thread / coroutine context / event loop.
- **11.2** The background sender uses **exactly one** concurrent flight per session. Parallel POSTs would race the `seq` ordering downstream (still functionally correct due to idempotency, but the logs view in the CLI would show batches out of order).
- **11.3** SDKs MUST never touch UI / main thread for log work after bootstrap returns.

## 12. JSON shape

The wire-format JSON shapes are normative; see `wire-protocol.md`. Key cross-language gotchas:

- The source-function field is `"func"`, not `"function"`. Use language-private aliases.
- Levels are lowercase strings, not integers.
- All timestamps are unix milliseconds (integer), never strings.
- Metadata is `map<string,string>`. Nested objects or non-string values are **not** part of v1.
- Optional fields SHOULD be omitted entirely rather than emitted as `null`. The server tolerates either.

## 13. Test expectations

A conforming SDK ships:

- **Unit tests** (language-native) covering JSON encoding, level parsing, queue drop semantics, backoff curve, discovery precedence.
- **End-to-end test** that spawns the `agentlog` server with a temp data dir, bootstraps the SDK pointed at it, emits sample logs, and verifies via `GET /v1/logs?session=…` that they round-tripped intact.
- A `make test-sdk-<lang>` target in the repo's top-level Makefile that runs the SDK's own tests.

Cross-language **conformance** tests live in `tests/conformance/` and exercise the wire protocol independently of any SDK. SDK tests need not duplicate those.

## 14. Versioning

- The wire protocol version is set by the URL prefix (`/v1`). SDKs targeting different wire versions MUST use different package versions and document the supported wire version in their README.
- SDK semver tracks the language API; the package version is unrelated to the wire version.
- Breaking changes within an SDK (renaming a public method, changing a parameter type) bump the SDK's major version even if the wire version is unchanged.
