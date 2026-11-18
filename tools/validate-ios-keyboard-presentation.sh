#!/usr/bin/env bash
set -euo pipefail

# UIKit aborts an iOS custom keyboard when a modal alert is constructed.
# Keep confirmation, menus and notices inside the keyboard's own view tree.
task_repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if task_matches=$(rg -n --glob '*.swift' '\b(UIAlertController|UIAlertAction)\b|(^|[^A-Za-z0-9_])present\s*\(' \
    "$task_repo_root/AI-Reply/ReplyKeyboard"); then
    printf '%s\n' 'Unsupported modal presentation in the keyboard:' "$task_matches" >&2
    exit 1
else
    task_status=$?
    if [ "$task_status" -ne 1 ]; then exit "$task_status"; fi
fi
printf '%s\n' 'Keyboard presentation guard passed.'
