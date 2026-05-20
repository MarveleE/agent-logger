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
#   2. Copy the Claude Code skills to ~/.claude/skills/ for global agent use.
#
# Note: there is no "auto-start on login" — run `agentlog start &` yourself,
# or wrap it in launchd / systemd / nssm if you want it supervised.

set -euo pipefail

FROM_SOURCE=false
PREFIX="/usr/local"

for arg in "$@"; do
    case "$arg" in
        --from-source) FROM_SOURCE=true ;;
        --prefix=*)    PREFIX="${arg#--prefix=}" ;;
        -h|--help)
            sed -n '2,16p' "$0"
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

echo "==> installing Claude Code skills (best-effort)"
SKILLS_SRC_DIR="$REPO_ROOT/.claude/skills"
SKILLS_DST_DIR="$HOME/.claude/skills"
if [ -d "$SKILLS_SRC_DIR" ]; then
    mkdir -p "$SKILLS_DST_DIR"
    for skill in "$SKILLS_SRC_DIR"/*/; do
        [ -d "$skill" ] || continue
        name="$(basename "$skill")"
        cp -R "$skill" "$SKILLS_DST_DIR/"
        echo "  -> $SKILLS_DST_DIR/$name"
    done
else
    echo "  [skip] $SKILLS_SRC_DIR not found"
fi

echo ""
echo "AgentLogger installed."
echo "  Binary:  $BIN_PATH"
echo "  Data:    ~/.agentlog/  (agentlog.sqlite, agentlog.pid, agentlog.port, server.log)"
echo ""
echo "Next:"
echo "  agentlog start &       # run the daemon in the background"
echo "  agentlog status"
echo "  agentlog --help"
