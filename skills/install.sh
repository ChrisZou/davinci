#!/bin/sh
# Installs the davinci skill for every agent found on this machine.
#
# The skill lives in this repo (skills/davinci). Claude Code gets a symlink to
# it; Codex and Hermes get symlinks to the Claude Code copy, the same layout the
# other shared skills here use — so editing the repo updates all three.
set -e
here="$(cd "$(dirname "$0")" && pwd)"
src="$here/davinci"

link() { # link <target> <link path>
  mkdir -p "$(dirname "$2")"
  if [ -e "$2" ] && [ ! -L "$2" ]; then
    echo "skip  $2（已存在且不是软链接，没有覆盖）"
    return
  fi
  ln -sfn "$1" "$2"
  echo "ok    $2 -> $1"
}

link "$src" "$HOME/.claude/skills/davinci"
[ -d "$HOME/.codex" ] && link "../../.claude/skills/davinci" "$HOME/.codex/skills/davinci"
[ -d "$HOME/.hermes" ] && link "../../.claude/skills/davinci" "$HOME/.hermes/skills/davinci"

command -v davinci >/dev/null 2>&1 || echo "提示：PATH 上还没有 davinci，在仓库根目录执行 ./start.sh skill（会把命令一起装好）"
