---
name: agentlogger-investigate
description: When the user reports any runtime / business problem in an app the project ships — bug, crash, blank screen, broken button, "doesn't work", network error, missing data, unexpected UI state, flow that should fire but didn't, feature you just implemented not behaving — DO NOT speculate from the source. First check whether the project is wired to AgentLogger (a local log capture daemon — look for `agentlog` on PATH, `~/.agentlogger/agentlog.sqlite`, or an AgentLogger SDK declared in any package manifest). If yes, pull the actual logs from the latest session **before** forming any hypothesis, then build the diagnosis on the evidence. Also use proactively right after the app has been (re)launched, after the user reproduces a bug, and before declaring a fix complete. Platform-agnostic — works for any client that ships logs via the AgentLogger wire protocol.
---

# Investigate bugs from AgentLogger logs

The most expensive mistake in agent-assisted debugging is **guessing from the
code instead of looking at runtime evidence**. The code shows what *should*
happen; the logs show what *did* happen. When AgentLogger is set up for the
project, querying logs is usually cheaper and far more accurate than reading
through suspects.

This skill makes that the reflex. The `agentlog` CLI is the same on every
platform — the only platform-specific bit is how you find the app's
identifier and where pinned config lives. Everything below is described in
neutral terms so it applies whether the app is native mobile, desktop, web,
embedded, or anything else that ships logs through an AgentLogger SDK.

## When to engage

Engage whenever the user describes a runtime symptom of an app the project
is shipping, even if they don't mention logs or debugging. Examples worth
recognising:

- "the login button doesn't work" / "tapping save does nothing"
- "I'm seeing a blank screen after onboarding"
- "the network call times out / 500s / returns the wrong shape"
- "it crashes on launch / when I do X"
- "the feature I just added isn't running" / "my new code isn't firing"
- "this regressed since yesterday's merge"
- "console output isn't appearing anywhere I can read it"
- "build succeeded but the screen is empty"

Also engage proactively after these moments:

- Right after the app has been (re)launched through any non-IDE flow —
  confirm it actually booted and is shipping logs before reporting success.
- Right after the user says "I reproduced it" — pull the matching session.
- Before declaring a fix complete — re-run, then look at the post-fix logs.

If the project clearly **does not** use AgentLogger (no `agentlog` binary,
no AgentLogger SDK in manifests, no `~/.agentlogger/agentlog.sqlite`), skip
this skill and debug normally. Don't push AgentLogger setup on the user
mid-bug unless they ask.

## Step 0 — Detect setup (10s, do this once per session)

Run a quick check before assuming logs are available:

```bash
command -v agentlog >/dev/null && agentlog server status
```

- `running` → ready, proceed to step 1.
- `stopped` with the daemon registered for auto-start → wait ~2 s and
  re-check; the supervisor should restart it.
- `stopped` with no supervisor → ask the user to run `agentlog server start &`
  (or whatever the project's "start the daemon" command is — `make
  server-start` in the canonical layout), then continue.
- `agentlog: command not found` → AgentLogger isn't installed on this
  machine. Note this once and fall back to normal debugging.

Also confirm the project actually ships logs to it — look for evidence that
**any** AgentLogger SDK is declared and bootstrapped. Patterns to grep for,
in priority order:

1. An AgentLogger entry in a package manifest (any ecosystem's manifest
   file — language doesn't matter).
2. A literal `AgentLogger.bootstrap` (or the SDK's equivalent init call) in
   the codebase.
3. A pinned endpoint key referencing AgentLogger in any platform's
   configuration file (e.g. an `AgentLoggerEndpoint` entry).

If no SDK is wired in, surface that to the user — the **lack of logs** is
the real problem, not the bug they're chasing.

## Step 1 — Identify the right session

You need the app's identifier — the value the SDK reports as `bundleId` on
the wire. Different platforms surface it differently: native mobile apps
have a reverse-DNS bundle/application id, multi-target projects carry one
per target, web apps often invent a logical name passed to `bootstrap`. If
the user didn't say which app, look in the nearest manifest or `bootstrap`
call site and extract the value being used.

Then:

```bash
agentlog sessions latest --bundle <app-id> --json
```

Three outcomes:

1. **Session returned** — note the `id`. Proceed to step 2.
2. **`no session found`** — the app never connected. Most likely causes,
   in order: the daemon wasn't running when the app launched · `bootstrap`
   wasn't called · the endpoint config (env var, pinned manifest value) is
   wrong · the app crashed before it could ship. Decide based on what you
   know about the launch sequence; if unsure, ask the user to re-launch
   the app with the daemon up and try again.
3. **`server is not running`** — go back to Step 0.

For "is the app even running right now?" use `--active`:

```bash
agentlog sessions list --bundle <app-id> --active
```

## Step 2 — Pull the evidence (in order of cost)

Build the picture in layers. Start narrow and widen only if needed — you
don't want to drown in 5000 lines of debug output.

**Always start with errors:**

```bash
agentlog logs --bundle <app-id> --level error --since 10m --json
```

If the user described a precise event ("when I tapped save"), use a tight
window and full-text search:

```bash
agentlog search "save" --bundle <app-id> --limit 50
agentlog logs --bundle <app-id> --grep <suspect-keyword> --since 5m
```

If errors aren't telling the whole story (silent failures, branch never
taken), widen to all levels around the moment of interest:

```bash
agentlog logs --bundle <app-id> --since 2m --order asc
```

When the app is currently running and you can reproduce live, `tail` is the
cleanest tool — it gracefully resolves the latest session:

```bash
agentlog tail --bundle <app-id>            # default text
agentlog tail --bundle <app-id> --json     # for piping
agentlog tail --bundle <app-id> --level warning   # only loud stuff
```

**Always pass `--json`** when you'll parse the result programmatically; the
output is NDJSON (one record per line) and stable.

## Step 3 — Build a hypothesis from the data, not from the code

The temptation is to read the file the user mentioned and pattern-match an
"obvious bug". Resist. Use the logs to answer these in order:

1. **Did the code path even run?** If you don't see any log lines from the
   feature in question, the bug is upstream — missing wiring, a condition
   gating the call, a view not mounted, a handler not bound, a feature
   flag off.
2. **What level did it stop at?** Look for the last log before the symptom
   appeared. Is the most recent line a warning ("falling back to…"), an
   error, or just silence?
3. **What does the metadata say?** AgentLogger captures `metadata`,
   `category`, `file`, `line`, and `func`. A single error log usually tells
   you the file:line where it happened — go there next.
4. **Has this regressed?** If the bug is new, compare the latest session's
   error pattern to an older one (`agentlog sessions list --bundle X`,
   then `agentlog logs --session <old-id>`). A diff between "yesterday's
   shape" and "today's shape" is more useful than reading commits.

Only after you have data, open the source files and verify the hypothesis.

## Step 4 — Verify the fix from the logs

Don't declare a fix done from the code. After applying a change:

1. Rebuild and relaunch the app via whatever flow the project uses.
2. Reproduce the original action.
3. `agentlog tail --bundle <app-id>` while reproducing — confirm the new
   code path emits the expected log line(s) and no new errors appear.
4. If you can, `agentlog logs --bundle <app-id> --level error --since 1m`
   and verify it's empty (or contains only known-unrelated noise).

A fix that builds clean but doesn't produce the right log evidence is not a
verified fix.

## Pattern catalogue

A few recurring shapes worth recognising at a glance.

### "Code never ran"
- `sessions latest` returns a session, but there is no log line from the
  feature.
- Likely: branch condition false, handler not bound, view/route not
  mounted, feature gated by flag, callback never invoked.
- Next move: `agentlog logs --bundle X --since 1m --order asc` to confirm
  upstream lines and see where the trace ends.

### "Silent network failure"
- Last log is something like `Network: GET /foo started`, with no
  subsequent success or error line.
- Likely: request canceled (view dismissed before completion), task/promise
  swallowed, response parsing bailed without logging.
- Next move: `agentlog logs --bundle X --category Network --since 2m` then
  inspect the call site for unmanaged async work.

### "Crash with no error log"
- The session's `lastSeenAt` stops abruptly with no `critical` line.
- Cause: the SDK is async — hard crashes can lose the in-memory queue.
- Cross-reference with the platform's native crash log (whatever the OS or
  runtime provides) and align timestamps with the last AgentLogger line
  received to localize the failure.

### "Logs show old run, not the one I just did"
- `sessions latest` returns a stale id; the new launch didn't register.
- Likely: the app was launched without the endpoint config wired in, or
  `bootstrap` no-op'd because the previous instance is still alive in
  memory. Force-terminate before relaunching and confirm a fresh session
  id is created.

### "Server is up but nothing arrives"
- `server status` is `running`, app claims it bootstrapped, no session shows.
- Likely: endpoint mismatch — the SDK couldn't resolve a reachable URL.
  Confirm whichever discovery layer the platform uses (compiled-in
  manifest value, env var, default loopback) actually points at the
  running daemon, and that the network between the device and the host
  isn't blocking the connection.

## Avoid these failure modes

- **Reading 1000 lines of debug** without a filter. Start with `--level
  error --since 10m`; widen only if needed.
- **Trusting `logs --bundle X` with no time bound** when the user just
  reproduced something — old sessions for the same app clutter the view.
  Always pair `--bundle` with `--since` or `--session`.
- **Skipping the session check** and querying `logs` directly. Without a
  session, you don't know whether the app even spoke to the daemon.
- **Concluding from a single error line** that you found the cause. One
  error often has a clean upstream warning that points to the actual gap.
- **Citing source code when logs would settle it.** Pull the evidence first;
  use code reading to verify, not to guess.

## Quick reference

```bash
agentlog server status                                          # is the daemon up?
agentlog sessions latest --bundle <id> --json                   # newest run
agentlog sessions list --bundle <id> --active                   # currently live?
agentlog logs --bundle <id> --level error --since 10m --json    # what blew up?
agentlog logs --bundle <id> --since 2m --order asc              # what happened, in order
agentlog logs --bundle <id> --grep <keyword>                    # focused window
agentlog search "<phrase>" --bundle <id>                        # FTS across history
agentlog tail --bundle <id>                                     # live stream
```

For full CLI reference, install instructions, data layout, and SDK
integration, see the project README. This skill is the **reflex**: when
something's broken at runtime, pull logs first.
