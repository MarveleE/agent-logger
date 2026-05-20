---
name: agentlogger-new-sdk
description: When a contributor wants to add an AgentLogger client SDK for a new platform — "add a Kotlin/Android SDK", "port AgentLogger to JavaScript / TypeScript / Dart / Flutter / Rust / Python", "implement the agentlogger client in <language>", "start a new agentlogger SDK", "write the Android/web/Flutter client" — this skill is the on-ramp. It walks you through the wire protocol + behavior contracts that ALL SDKs must obey, picks a file layout, runs through the 13 mandatory behaviors with platform-appropriate patterns, and finishes with the tests + Makefile wiring you need before the PR is mergeable. Engage at the start of any "I'm adding a new SDK" thread, even if the user only mentioned the target language without saying the word "SDK". Do NOT engage when the user is debugging an existing app (that's `agentlogger-investigate`) or just calling the existing SDK from their code.
---

# Adding a new AgentLogger SDK

The wire protocol is the only hard contract between an SDK and the server. The behavior spec is the only soft contract between SDKs. If your implementation passes both, the language you wrote it in doesn't matter — it's a first-class AgentLogger client.

This skill is the recipe. The Swift SDK at `sdks/swift/` is the reference implementation; consult it when a paragraph here is ambiguous.

## When to use this skill

Engage when the user says any of:

- "add a Kotlin / Android / Java / JVM SDK"
- "I want to port AgentLogger to JavaScript / TypeScript / browser / Node"
- "write a Dart / Flutter client"
- "start a Rust / Python / Go client for AgentLogger"
- "implement the SDK for <platform>"

Or proactively when the conversation is clearly about creating one — the language alone is enough of a signal once "SDK" / "client" / "port" is in the air.

Do **not** engage when the user is debugging an existing app (use `agentlogger-investigate`) or asking about the existing Swift SDK API surface.

## Step 1 — Read the contracts (15 minutes)

Two documents, both short:

- `docs/wire-protocol.md` — HTTP endpoints, JSON shapes, status codes, idempotency rules.
- `docs/sdk-behavior.md` — the 13 numbered clauses that every SDK obeys.

Read them in this order. Re-read § 1 (Bootstrap), § 3 (Queue), § 4 (Batching), § 5 (Backoff), and § 7 (Discovery) carefully — those are where 90% of subtle bugs hide.

If the user hasn't read them yet, link both before going further; don't skip ahead.

## Step 2 — Pick the layout

All new SDKs go under `sdks/<lang>/` as a sibling to `sdks/swift/`. Use the language's conventional structure for that ecosystem. Reasonable defaults:

### Kotlin / Android
```
sdks/kotlin/
├── settings.gradle.kts
├── build.gradle.kts                     # multi-module if Android + JVM common
├── README.md
├── CHANGELOG.md
├── agentlogger/                         # the library module
│   ├── build.gradle.kts
│   └── src/main/kotlin/io/agentlogger/
│       ├── AgentLogger.kt               # public facade
│       ├── Configuration.kt
│       ├── LogEntry.kt
│       ├── LogLevel.kt
│       ├── Session.kt
│       ├── LogQueue.kt                  # Channel-backed
│       ├── HttpTransport.kt
│       ├── Discovery.kt
│       └── Backoff.kt
└── agentlogger/src/test/kotlin/         # unit tests
```

### JavaScript / TypeScript
```
sdks/js/
├── package.json
├── tsconfig.json
├── README.md
├── CHANGELOG.md
├── src/
│   ├── index.ts                         # re-export public API
│   ├── agentLogger.ts                   # facade
│   ├── config.ts
│   ├── logEntry.ts
│   ├── logLevel.ts
│   ├── session.ts
│   ├── logQueue.ts
│   ├── httpTransport.ts
│   ├── discovery.ts
│   └── backoff.ts
└── test/
    └── *.test.ts                        # vitest preferred
```

### Dart / Flutter
```
sdks/dart/
├── pubspec.yaml
├── README.md
├── CHANGELOG.md
├── lib/
│   ├── agent_logger.dart                # the import root
│   └── src/
│       ├── agent_logger.dart            # facade
│       ├── configuration.dart
│       ├── log_entry.dart
│       ├── log_level.dart
│       ├── session.dart
│       ├── log_queue.dart
│       ├── http_transport.dart
│       ├── discovery.dart
│       └── backoff.dart
└── test/
    └── *_test.dart
```

For other languages, follow the local ecosystem convention. The names above (`Configuration`, `LogEntry`, `LogLevel`, `Session`, `LogQueue`, `HttpTransport`, `Discovery`, `Backoff`, public facade) are the units the Swift SDK has — mirror them so reviewers can scan the diff against the reference.

## Step 3 — Implement, in this order

Each line below corresponds to a section in `docs/sdk-behavior.md`. Tick them off as you go.

1. **LogLevel enum** (§ 12). Seven members. Codec uses lowercase strings; tolerate the numeric form on decode.
2. **LogEntry data class** (§ 12, wire-protocol § "LogEntry"). Field name on the wire is `"func"`, not `"function"`. Use a language-private alias for the property.
3. **Session metadata** (§ 1.5). Synchronous collection at bootstrap. See the per-language pattern below for `bundleId` / `deviceId` / `osVersion` / `appVersion`. Generate the session id (UUID) here.
4. **Bounded queue with drop-oldest** (§ 3). Implement the drop counter and the "first entry of the next batch gets `metadata._dropped`" rule.
5. **Backoff helper** (§ 5). `1, 2, 4, 8, 16, 30, 30, …` seconds; reset on success.
6. **HTTP transport** (§ 4, § 12). One concurrent flight per session. JSON encode/decode round-trips. Use the platform's standard HTTP stack — prefer no third-party dep.
7. **Discovery** (§ 7). Read pinned config → env var → platform localhost. Probe `/v1/health` with a 3 s timeout for each candidate.
8. **Sender loop** (§ 4). Async task that:
   - resolves the endpoint
   - registers the session (POST `/v1/sessions`)
   - drains the queue in batches of ≤ batchSize, flushing on the batchInterval timer too
   - retries failed batches with backoff; re-runs discovery after sustained failure
   - handles 404 by re-registering the session (§ 6.3)
9. **Public facade** (§ 2). `bootstrap(config)`, level methods (`trace`/`debug`/…/`critical`), `category(name)`, `currentSessionID`, `shutdown()`. All synchronous, all non-blocking.
10. **Lifecycle hook** (§ 8). Platform-appropriate "going to background" → flush.
11. **Disabled mode** (§ 9). When configured, every path becomes a no-op.
12. **Internal error logging** (§ 10). Goes to the platform's native log channel with `agentlogger:` prefix; rate-limited.
13. **Threading guarantees** (§ 11). Single concurrent POST per session; public API thread-safe.

## Step 4 — Tests

Three layers, in order of importance:

1. **Unit tests** (your SDK only):
   - LogLevel round-trips for both numeric and string inputs
   - LogEntry encodes with `"func"` as the source-function key
   - Queue at capacity drops oldest and increments the drop counter
   - Backoff returns `1, 2, 4, 8, 16, 30, 30` over seven calls and resets cleanly
   - Discovery reads the platform's pinned config and env var with correct precedence

2. **SDK end-to-end** (against a real `agentlog` server):
   - `make build` (the top-level Makefile) → `bin/agentlog`
   - Start server with `--data-dir <tempdir> --port <random>` in a fixture
   - Bootstrap the SDK pointed at it
   - Emit a small variety of logs (info + error with metadata + categorised entry)
   - Wait long enough for one batch (≥ batchInterval + a small grace)
   - Query `GET /v1/logs?session=<id>` and assert all entries round-tripped
   - Teardown: stop server, clean tempdir

3. **Conformance** (already exists in `tests/conformance/`; don't duplicate). Your SDK's e2e implicitly exercises a subset, but you don't need to reproduce the curl-driven tests.

Wire into the top-level Makefile:

```makefile
.PHONY: test-sdk-<lang>
test-sdk-<lang>:
	<language-native command, e.g. cd sdks/<lang> && gradle test>

.PHONY: test-sdks
test-sdks: test-sdk-swift test-sdk-<lang>
```

## Step 5 — Demo + docs

- Add `examples/<platform>/` with the smallest reasonable host app:
  - Android: one Activity, one button per log level
  - Web: one HTML page with buttons
  - Flutter: one stateful widget with buttons
  - Other: equivalent "smallest reproducible app"
- Add `sdks/<lang>/README.md` covering: install, bootstrap, log API, real-device setup, link back to top-level `docs/wire-protocol.md` and `docs/sdk-behavior.md`.
- Add `sdks/<lang>/CHANGELOG.md` starting at 0.1.0.
- Update the top-level README's Roadmap row to mark this SDK as shipped.

## Per-language patterns

### Kotlin / Android

- **Concurrency**: a `CoroutineScope(SupervisorJob() + Dispatchers.IO)` with one `launch { for (entry in channel) sendBatch(...) }` consumer. `Channel(capacity = queueCapacity, BufferOverflow.DROP_OLDEST)` gives you the drop-oldest semantics for free; track the drop count via `channel.trySend(...).isFailure`.
- **HTTP**: prefer `HttpURLConnection` (stdlib) over OkHttp to keep the SDK dep-free. If users want OkHttp, they can swap the transport later.
- **Bundle id**: `applicationContext.packageName`. **App version**: `packageManager.getPackageInfo(packageName, 0).versionName`. **Device id**: `Settings.Secure.getString(contentResolver, Settings.Secure.ANDROID_ID)` (acknowledge the caveats — it's stable per signing key + user).
- **Emulator detection**: `Build.FINGERPRINT.contains("generic")` or `Build.PRODUCT in setOf("sdk_gphone64_arm64", "sdk_gphone_x86_64", ...)`.
- **Default endpoint on emulator**: `http://10.0.2.2:8765` (the AVD's view of the host's loopback).
- **Pinned config**: read `<meta-data android:name="AgentLoggerEndpoint">` via `packageManager.getApplicationInfo(packageName, GET_META_DATA).metaData`.
- **Lifecycle**: `ProcessLifecycleOwner.get().lifecycle.addObserver { onStateChanged → ON_STOP → flush() }`. Requires `androidx.lifecycle:lifecycle-process` — if you want zero deps, register an `ActivityLifecycleCallbacks` on the `Application` instead.

### JavaScript / TypeScript (browser-first, Node opt-in)

- **Concurrency**: an `Array<LogEntry>` plus `setInterval(flush, batchInterval)` and a "flush early if length ≥ batchSize" check after each push. The Promise from `fetch()` is the in-flight tracker; serialize with a simple `let inFlight: Promise<unknown> | null`.
- **HTTP**: `fetch` (browser + modern Node). Use `AbortController` for the 10 s timeout. On `pagehide` / `visibilitychange → hidden`, switch to `navigator.sendBeacon` for the final flush so the browser doesn't drop the request.
- **Bundle id**: there is no canonical one. Require the user to pass `appId` in the configuration, default to `location.host`.
- **Device id**: generate a UUID once and persist to `localStorage`. Fall back to a per-session UUID if `localStorage` is unavailable (private browsing).
- **OS version**: `navigator.userAgent` parsed minimally; don't pull in a UA parser dep.
- **Pinned config**: `document.querySelector('meta[name="agentlogger-endpoint"]')?.content` or `(window as any).AGENTLOGGER_ENDPOINT`.
- **Default endpoint**: `http://localhost:8765` (you can't reach the dev's Mac from another device's browser without explicit config — that's a user-education matter).
- **CORS**: the server doesn't enable CORS by default. The SDK should detect this gracefully (`fetch` throws TypeError → log once to console, then disable transport) and the README should tell users to launch with `--enable-cors` when added.
- **Build**: emit both ESM and CJS via `tsup` or rollup. No transitive deps.

### Dart / Flutter

- **Concurrency**: a `StreamController<LogEntry>.broadcast(sync: false)` plus a `Timer.periodic` for time-based flush, manual flush when buffer length ≥ batchSize.
- **HTTP**: `dart:io`'s `HttpClient` for native, `dart:html` for web. Or `package:http` if you're willing to take one dep (Flutter community default). For zero-dep, conditional imports via `if (dart.library.html)`.
- **Bundle id / device info**: `package_info_plus` + `device_info_plus` are the de-facto standard. They are real deps. If you want zero deps, use a platform-channel method on iOS/Android and `window.location.host` on web — more work but cleaner.
- **Pinned config**: `const String.fromEnvironment('AGENTLOGGER_ENDPOINT', defaultValue: '')` baked at compile time via `--dart-define`. Plus a runtime override via a configuration parameter.
- **Lifecycle**: `WidgetsBindingObserver.didChangeAppLifecycleState(state)` — flush on `AppLifecycleState.paused`.
- **Web fallback**: `kIsWeb` branch uses fetch via `dart:html`. Most platform-specific code lives behind conditional imports.

### Other languages

Follow the same recipe. The Swift SDK's `Core.swift` (the sender loop) and `LogQueue.swift` (the bounded channel) are the parts most worth reading. Everything else is platform glue.

## Validation — when is the SDK ready to merge?

A reviewer will check:

- [ ] `docs/wire-protocol.md` and `docs/sdk-behavior.md` referenced explicitly (not just paraphrased)
- [ ] All 13 behaviors implemented and tested (see § 3 above)
- [ ] No third-party HTTP/JSON deps unless absolutely necessary (justify in the PR if you took one)
- [ ] `bootstrap()` is idempotent; called twice does nothing the second time
- [ ] Log calls non-blocking; verified with a stress test that the SDK ingests ≥ 10 000 entries with the server stopped without affecting host throughput
- [ ] Queue drops oldest under saturation and reports `_dropped` on next batch
- [ ] Discovery walks all three layers in the documented order
- [ ] Lifecycle hook flushes before the app backgrounds
- [ ] End-to-end test passes via `make test-sdk-<lang>`
- [ ] `examples/<platform>/` builds and runs against a local `agentlog` server
- [ ] Top-level `README.md` Roadmap entry updated
- [ ] `sdks/<lang>/README.md` and `CHANGELOG.md` present

## Common pitfalls

- **Wrong field name on the wire**: the source-function field is `"func"`. Forgetting this and emitting `"function"` means the server stores it as null. Easy to miss in fixtures.
- **Forgetting to register the session before the first batch**: the server returns 404, your sender ends up in a backoff loop forever. The fix is § 6.3 — re-register on 404.
- **Background lifecycle hook holding the main thread**: don't `await` the flush from the lifecycle callback. Best-effort dispatch with a short deadline.
- **Sending the dropped count as a normal log entry**: that would be self-referential and would itself need to be queued. The contract is § 3.4 — piggyback in metadata, not a new entry.
- **Generating a new session id after a 404**: that fragments the user's logs into "phantom" sessions. Always reuse the same id.
- **Per-call Task / coroutine spawn from the public API**: cheap but not free; over a million log calls it shows up. The Swift SDK pushes directly into an AsyncStream continuation (synchronous yield) instead. Aim for the same in your language.

## Pointer back

The reference Swift SDK is in `sdks/swift/`. Files to study, in this order:

1. `Sources/AgentLogger/LogEntry.swift` — wire types
2. `Sources/AgentLogger/LogQueue.swift` — bounded channel
3. `Sources/AgentLogger/Backoff.swift` — exponential backoff
4. `Sources/AgentLogger/HTTPTransport.swift` — POST + retry primitives
5. `Sources/AgentLogger/Discovery.swift` — three-layer resolution
6. `Sources/AgentLogger/Core.swift` — the sender loop (the hardest file)
7. `Sources/AgentLogger/AgentLogger.swift` — the public facade
8. `Tests/AgentLoggerTests/` — what the tests look like
