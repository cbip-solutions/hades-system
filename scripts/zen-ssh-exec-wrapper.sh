#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -u

ALLOWLIST_FILE="${ZEN_ALLOWLIST_FILE:-/etc/zen-swarm/ssh-exec.allowlist}"
FORBIDDEN=$(printf '%s' ';&|$`<>(){}[]"'"'"'*?~')

if [ -n "${SSH_TTY:-}" ]; then
  echo "zen-ssh-exec-wrapper: PTY refused" >&2
  exit 126
fi

cmd="${SSH_ORIGINAL_COMMAND:-}"
if [ -z "$cmd" ]; then
  echo "zen-ssh-exec-wrapper: empty command" >&2
  exit 126
fi

i=0
while [ "$i" -lt "${#cmd}" ]; do
  c="${cmd:$i:1}"
  case "$FORBIDDEN" in
    *"$c"*)
      echo "zen-ssh-exec-wrapper: forbidden char $c" >&2
      exit 126
      ;;
  esac
  i=$((i + 1))
done

if [ ! -r "$ALLOWLIST_FILE" ]; then
  echo "zen-ssh-exec-wrapper: allowlist unreadable" >&2
  exit 126
fi
allowed=0
while IFS= read -r line; do
  case "$line" in
    ''|'#'*) continue ;;
  esac
  case "$line" in
    *' *')
      prefix="${line% \*}"
      if [ "$cmd" = "$prefix" ] || [ "${cmd#"$prefix "}" != "$cmd" ]; then
        allowed=1
        break
      fi
      ;;
    */\*)
      prefix="${line%\*}"   # keeps the trailing '/'
      if [ "${cmd#"$prefix"}" != "$cmd" ]; then
        allowed=1
        break
      fi
      ;;
    *)
      if [ "$cmd" = "$line" ]; then
        allowed=1
        break
      fi
      ;;
  esac
done < "$ALLOWLIST_FILE"

if [ "$allowed" -eq 0 ]; then
  echo "zen-ssh-exec-wrapper: command not in allowlist" >&2
  exit 126
fi

if [ "${ZEN_CWD_REQUESTED:-0}" = "1" ] && [ -z "${ZEN_CWD:-}" ]; then
  echo "zen-ssh-exec-wrapper: ZEN_CWD_REQUESTED but ZEN_CWD empty" \
       "(sshd AcceptEnv ZEN_CWD missing? see wrapper header)" >&2
  exit 126
fi
target_dir="${ZEN_CWD:-$HOME}"
cd "$target_dir" 2>/dev/null || {
  echo "zen-ssh-exec-wrapper: cwd $target_dir unavailable" >&2
  exit 126
}

exec /bin/sh -c "$cmd"
