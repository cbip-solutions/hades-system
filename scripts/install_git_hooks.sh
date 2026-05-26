#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT"

HOOK_SRC="$REPO_ROOT/.githooks/pre-commit-dco"

if [ ! -f "$HOOK_SRC" ]; then
    echo "ERROR: DCO hook source missing: $HOOK_SRC" >&2
    echo "       Restore .githooks/pre-commit-dco from git before installing." >&2
    exit 1
fi

chmod +x "$HOOK_SRC"

configured_hooks_path="$(git config --get core.hooksPath || true)"
if [ "$configured_hooks_path" = ".githooks" ]; then
    if [ ! -x "$REPO_ROOT/.githooks/pre-commit" ]; then
        echo "WARN: core.hooksPath=.githooks but dispatcher .githooks/pre-commit missing or not executable." >&2
        echo "      Run: make install-hooks" >&2
        exit 1
    fi
    echo "OK: DCO hook active via .githooks dispatcher (core.hooksPath=.githooks)."
    echo "    Source: $HOOK_SRC"
    echo "    Dispatcher: $REPO_ROOT/.githooks/pre-commit"
    exit 0
fi

HOOK_DST="$(git rev-parse --git-path hooks/pre-commit)"
case "$HOOK_DST" in
    /*) : ;;                       # already absolute
    *)  HOOK_DST="$REPO_ROOT/$HOOK_DST" ;;
esac
HOOK_DIR="$(dirname "$HOOK_DST")"

mkdir -p "$HOOK_DIR"

if [ -e "$HOOK_DST" ] || [ -L "$HOOK_DST" ]; then
    if grep -q "pre-commit-dco" "$HOOK_DST" 2>/dev/null; then
        echo "OK: DCO hook already installed (chain detected at $HOOK_DST)."
        exit 0
    fi

    tmp="$(mktemp)"
    cp "$HOOK_DST" "$tmp"
    cat > "$HOOK_DST" <<EOF
#!/usr/bin/env bash
# Auto-chained by scripts/install_git_hooks.sh (Plan 15 phase C-14).
# Existing hook runs first; if it exits 0, the DCO check runs second.
set -euo pipefail
$(cat "$tmp")
exec "$HOOK_SRC" "\$@"
EOF
    chmod +x "$HOOK_DST"
    rm -f "$tmp"
    echo "OK: chained existing pre-commit hook with DCO check at $HOOK_DST."
    exit 0
fi

if ln -s "$HOOK_SRC" "$HOOK_DST" 2>/dev/null; then
    echo "OK: symlinked DCO pre-commit hook at $HOOK_DST -> $HOOK_SRC."
else
    cp "$HOOK_SRC" "$HOOK_DST"
    chmod +x "$HOOK_DST"
    echo "OK: copied DCO pre-commit hook to $HOOK_DST (symlink unavailable)."
fi

exit 0
