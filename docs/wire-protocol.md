# AgentLogger Wire Protocol — v1

This is the **only** hard contract between SDKs and the `agentlog` server. If your SDK obeys this document end-to-end, it's a conforming client. Any change here is a versioned breaking change.

- **Base URL**: whatever the user has configured (`http://127.0.0.1:8765` by default).
- **Auth**: none. The server is intended to run on the developer's own machine and bind to loopback or the local LAN.
- **All endpoints are versioned**: paths begin with `/v1/`. A future breaking change will introduce `/v2/`; the server may serve both for a transition window.
- **Content type**: `application/json; charset=utf-8` on every request and response that has a body.
- **Time format**: every timestamp is an **integer of milliseconds since the Unix epoch** (UTC). Never ISO-8601 over the wire.
- **IDs**: opaque strings. SDKs SHOULD generate session IDs as UUIDs (any variant) but the server treats them as strings.

---

## Type reference

### Level enum

| String (wire) | Integer (internal) | Meaning |
|---|---|---|
| `"trace"` | 0 | Most verbose, usually filtered out |
| `"debug"` | 1 | Diagnostic detail |
| `"info"` | 2 | Routine, expected events |
| `"notice"` | 3 | Noteworthy but not problematic |
| `"warning"` | 4 | Potentially harmful, recoverable |
| `"error"` | 5 | Failure of a discrete operation |
| `"critical"` | 6 | Failure that may compromise the app |

On the wire, levels are **strings** (lowercase, exactly as above). The server tolerates a numeric form on input (e.g. `5` for `"error"`) but always emits the string form.

`"warning"` and `"warn"` are both accepted on input; output uses `"warning"`.

### Session

```json
{
  "id":         "uuid string",
  "bundleId":   "com.example.app",
  "deviceId":   "device-or-host-uuid",
  "deviceName": "iPhone 16 Simulator",
  "deviceKind": "simulator",        // open vocabulary; see below
  "platform":   "ios",              // optional: ios|macos|tvos|watchos|android|web|node|linux|windows|…
  "osVersion":  "iOS 18.0",
  "appVersion": "1.2.3",
  "appBuild":   "456",              // optional: build number / git SHA / equivalent
  "startedAt":  1717777777000,      // unix ms
  "lastSeenAt": 1717777777999,      // unix ms; server-managed
  "metadata":   { "any": "k/v" }    // optional, map<string,string>
}
```

`bundleId` and `deviceId` are the only **required** fields when creating a session. All others are optional but recommended.

**Cross-platform semantics**. Names use iOS terminology for historical reasons; the *meaning* is universal. Each SDK fills these from whatever its platform exposes:

| Field | Logical meaning | Reference values per platform |
|---|---|---|
| `bundleId` | Stable identifier for the app product | iOS bundle id · Android `applicationId` · web: user-supplied app name or `location.host` · Node: `package.json#name` |
| `deviceId` | Stable identifier for the machine / instance | iOS `identifierForVendor` · Android `ANDROID_ID` · web: persisted localStorage UUID · server: hostname or instance id |
| `deviceName` | Human-readable label | iOS `UIDevice.name` / device model · Android `Build.MODEL` · web: User-Agent summary · server: hostname |
| `deviceKind` | Form-factor / origin category. Open enum — recommended values: `simulator`, `emulator`, `device`, `browser`, `desktop`, `server`, `container`, `embedded`. SDKs MAY emit other strings; the server stores them as-is. |
| `platform` | Runtime category. Recommended values: `ios`, `macos`, `tvos`, `watchos`, `android`, `web`, `node`, `linux`, `windows`, `embedded`. The CLI filters case-sensitively. |
| `osVersion` | OS / runtime version (free-form string) | iOS `UIDevice.systemVersion` · Android `Build.VERSION.RELEASE` · web: parsed UA · server: `os.release()` |
| `appVersion` | Marketing version (semver-style) | iOS `CFBundleShortVersionString` · Android `versionName` · web: build-time injected |
| `appBuild` | Build number (distinct from `appVersion`) | iOS `CFBundleVersion` · Android `versionCode` · web / server: git short SHA or CI build id |

`bundleId` is the wire-level name; SDKs are free to expose more idiomatic names in their language API (`appId`, `applicationId`) as long as the JSON field is `bundleId`.

### LogEntry

```json
{
  "seq":      42,                            // required: monotonic int64 per session
  "ts":       1717777777999,                 // required: unix ms
  "level":    "info",                        // required: see Level enum
  "category": "Network",                     // optional: free-form tag
  "message":  "GET /users → 200",            // required: human-readable
  "metadata": { "k": "v" },                  // optional, map<string,string>
  "file":     "Service.swift",               // optional: source file (basename preferred)
  "line":     42,                            // optional: source line
  "func":     "fetchUsers()"                 // optional: source function name
}
```

**Field naming note**: the source-function field is `"func"`, not `"function"`. JS, Kotlin, and Dart reserve `function`/`fun` — SDKs must use the literal string `"func"` on the wire while exposing whatever they like in the language API.

A `LogEntry` only carries the fields above when sent from a client. When the server returns it (in query results), it additionally includes:

```json
{
  "id":        17,                  // server-assigned auto-increment primary key
  "sessionId": "session-uuid"
}
```

SDKs **MUST NOT** send `id` or `sessionId` inside the log entry; the URL carries the session id, and the server assigns `id`.

### Error response

Any 4xx or 5xx response has body:

```json
{ "error": "human-readable message" }
```

---

## SDK endpoints

These four endpoints are what an SDK uses. Everything else is for the CLI / human operators.

### `GET /v1/health`

Liveness probe. Used during discovery to confirm the server is reachable.

- Request: no body.
- Response `200 OK`:
  ```json
  { "ok": true, "ts": 1717777777999 }
  ```

SDKs SHOULD use this with a short timeout (e.g. 1–3 s) during endpoint resolution before doing anything else.

### `POST /v1/sessions`

Register or upsert a session. Idempotent on `id`.

- Request body: a Session object. `bundleId` and `deviceId` are required; if `id` is absent the server generates one; if `startedAt` is absent the server uses "now"; `lastSeenAt` is always set by the server.
- Response `201 Created`: the full Session as stored.
- Conflict behavior: if `id` already exists, the server updates `lastSeenAt` and returns the stored row. **It does not overwrite** `bundleId`/`deviceId`/`startedAt`/etc. on conflict — that's deliberate so a re-bootstrap doesn't lose history.

SDKs SHOULD register the session before sending any log batches. The reference SDK serializes register-then-flush.

### `POST /v1/sessions/{id}/logs`

Append a batch of log entries.

- Path param `id`: a session id that must already exist (404 otherwise).
- Request body:
  ```json
  { "batch": [ <LogEntry>, <LogEntry>, ... ] }
  ```
- Limits: at most **1024 entries** per batch. Exceeding returns `413 Payload Too Large`.
- Idempotency: `(session_id, seq)` is unique. Duplicates are silently dropped (`INSERT OR IGNORE` semantics). This means SDKs MAY retry a batch after a network failure without dedup logic on their side, as long as the `seq` for each entry is preserved.
- Response `202 Accepted`:
  ```json
  { "inserted": 3, "received": 4 }
  ```
  `inserted` may be less than `received` when entries were deduplicated by `(session_id, seq)`.
- Empty batch (`{"batch": []}`) returns `202` with `inserted=0`.

**Sequence numbers**: `seq` must be a monotonically increasing 64-bit integer per session. SDKs SHOULD start at 1 and increment by 1. The server uses `seq` only for dedup; it does not require strict monotonicity, only uniqueness within a session.

### `PUT /v1/sessions/{id}/heartbeat`

Update `lastSeenAt` without shipping any logs. Useful when an app has been quiet for a while but the agent still wants to know it's alive.

- Request: no body.
- Response: `204 No Content` on success, `404 Not Found` if the session id is unknown.

Heartbeats are **optional**. The server bumps `lastSeenAt` on every successful log ingest anyway, so an SDK that logs frequently doesn't need a separate heartbeat. The reference SDK does not send heartbeats in v1.

---

## CLI / operator endpoints

These are not part of the SDK contract but documented here for completeness.

| Method | Path | Purpose |
|---|---|---|
| `GET`  | `/v1/sessions` | List sessions (filter via `?bundle=`, `?device=`, `?active=true&activeWithin=<go-duration>`, `?limit=`) |
| `GET`  | `/v1/sessions/latest?bundle=ID` | Most recent session for a bundle |
| `GET`  | `/v1/sessions/{id}` | Session details |
| `GET`  | `/v1/logs` | Query logs (filters: `?session=`, `?bundle=`, `?level=`, `?category=`, `?since=`, `?from=`, `?to=`, `?grep=`, `?limit=`, `?order=asc\|desc`, `?cursor=<lastId>`) |
| `GET`  | `/v1/logs/search?q=…` | Full-text search across messages (SQLite FTS5) |
| `POST` | `/v1/db/prune` | `{ "before": "7d" }` → delete log entries older than a Go duration or absolute unix ms |
| `POST` | `/v1/db/reset` | Delete all sessions and logs; no body |

Query responses wrap results: `{"sessions": [...]}`, `{"logs": [...]}`.

---

## HTTP semantics SDKs should expect

- `200 OK` / `201 Created` / `202 Accepted` / `204 No Content` are success.
- `400 Bad Request` — client sent malformed JSON, unknown field (the server uses `DisallowUnknownFields`), or invalid query param.
- `404 Not Found` — session id unknown.
- `413 Payload Too Large` — batch over 1024 entries.
- `500 Internal Server Error` — bug or storage problem; safe to retry with backoff.

SDKs should treat 5xx and network-level failures as transient (retry with exponential backoff). 4xx other than 404 indicate a bug in the client and should not be retried.

---

## Versioning policy

The `/v1` prefix is part of the contract. Compatible additions (new optional fields, new endpoints) ship under `/v1` indefinitely. Breaking changes — removing/renaming a field, changing a status code, changing dedup semantics — require `/v2`, and the server promises to serve `/v1` in parallel for at least one minor release.

SDKs SHOULD probe `GET /v1/health` at startup. A future revision may add a `{"wire_version": "1.x"}` field; treat its absence as "v1, no advertised minor".
