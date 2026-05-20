#!/usr/bin/env bash
# AgentLogger installer.
#
#   curl -fsSL https://raw.githubusercontent.com/<org>/agentlogger/main/scripts/install.sh | bash
#
# Or from a local checkout:
#   ./scripts/install.sh [--from-source]
#
# Steps:
#   1. Place the agentlog binary at /usr/local/bin/agentlog
#      (--from-source builds it via `make build`; otherwise downloads release)
#   2. Install the launchd plist (~/Library/LaunchAgents/com.agentlogger.daemon.plist)
#      so the server auto-starts on login.
#   3. Copy the Claude Code skill to ~/.claude/skills/agentlogger.md (best-effort).

set -euo pipefail

FROM_SOURCE=false
PREFIX="/usr/local"

for arg in "$@"; do
    case "$arg" in
        --from-source) FROM_SOURCE=true ;;
        --prefix=*)    PREFIX="${arg#--prefix=}" ;;
        -h|--help)
            sed -n '2,18p' "$0"
            exit 0
            ;;
        *)
            echo "unknown arg: $arg" >&2
            exit 2
            ;;
    esac
done

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_PATH="$PREFIX/bin/agentlog"

if [ "$FROM_SOURCE" = true ]; then
    echo "==> building from source"
    cd "$REPO_ROOT"
    make build
    sudo install -d "$PREFIX/bin"
    sudo install -m 0755 "$REPO_ROOT/bin/agentlog" "$BIN_PATH"
else
    echo "==> release downloads are not yet published"
    echo "    rerun with --from-source (from a checked-out repo) for now."
    exit 1
fi

echo "==> registering launchd job"
"$BIN_PATH" server install

echo "==> installing Claude Code skill (best-effort)"
SKILL_SRC="$REPO_ROOT/.claude/skills/agentlogger.md"
SKILL_DST="$HOME/.claude/skills/agentlogger.md"
if [ -f "$SKILL_SRC" ]; then
    mkdir -p "$(dirname "$SKILL_DST")"
    cp "$SKILL_SRC" "$SKILL_DST"
    echo "  -> $SKILL_DST"
else
    echo "  [skip] skill source not found"
fi

echo ""
echo "AgentLogger installed."
echo "  Binary:       $BIN_PATH"
echo "  Auto-start:   ~/Library/LaunchAgents/com.agentlogger.daemon.plist"
echo "  Data:         ~/Library/Application Support/AgentLogger/"
echo ""
echo "Try:"
echo "  agentlog server status"
echo "  agentlog --help"
