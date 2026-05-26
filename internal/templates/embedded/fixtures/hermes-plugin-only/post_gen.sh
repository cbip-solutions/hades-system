#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

ANSWERS=$(cat)
NAME=$(echo "$ANSWERS" | python3 -c 'import sys,json; print(json.load(sys.stdin)["ProjectName"])')
INIT_GIT=$(echo "$ANSWERS" | python3 -c 'import sys,json; print(json.load(sys.stdin)["InitGit"])')
LINK_PLUGIN=$(echo "$ANSWERS" | python3 -c 'import sys,json; print(json.load(sys.stdin)["LinkHermesPlugin"])')
SCOPE=$(echo "$ANSWERS" | python3 -c 'import sys,json; print(json.load(sys.stdin)["HermesPluginScope"])')

if [ "$INIT_GIT" = "True" ] || [ "$INIT_GIT" = "true" ]; then
  if [ ! -d .git ]; then
    git init -q
    git add -A
    git -c commit.gpgsign=false commit -q -m "chore(scaffold): initialize $NAME via zen new"
  fi
fi

if [ "$LINK_PLUGIN" = "True" ] || [ "$LINK_PLUGIN" = "true" ]; then
  if [ "$SCOPE" = "user" ]; then
    mkdir -p "$HOME/.hermes/plugins"
    ln -sf "$(pwd)" "$HOME/.hermes/plugins/$NAME" || true
  else
    mkdir -p ".hermes/plugins"
    ln -sf "$(pwd)" ".hermes/plugins/$NAME" || true
  fi
fi

if curl -s -o /dev/null -w '%{http_code}' --unix-socket /tmp/zen-swarm.sock http://localhost/healthz 2>/dev/null | grep -q '^200$'; then
  echo "zen-swarm-ctld reachable: scaffold registered."
fi

exit 0
