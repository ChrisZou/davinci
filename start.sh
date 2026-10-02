#!/usr/bin/env bash
# davinci 的构建与启动。
#
#   ./start.sh [serve 参数]   构建（前端没改就跳过）并启动服务，打开浏览器
#                             例：./start.sh --port 7790
#   ./start.sh build          只构建 bin/davinci（前端内嵌进去）
#   ./start.sh dev            开发模式：Go 服务 + Vite 热更新，Ctrl-C 一起退出
#   ./start.sh install        构建，并把 davinci 链接到 $GOPATH/bin（命令行和 AI 用它）
#   ./start.sh skill          给 AI Agent 装 skill：构建、链接 davinci 命令，再装给 Claude Code / Codex / Hermes
#   ./start.sh test           go vet、go test 和前端类型检查
#   ./start.sh clean          删掉构建产物
#
# 需要：Go 1.26+、Node 20+、pnpm。服务端出图会用到 node。
set -euo pipefail
cd "$(dirname "$0")"

BIN=bin/davinci

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "缺少 ${1}：${2}" >&2; exit 1; }
}

# 前端：依赖没装就装，源码比上次构建新才重建。
build_web() {
  need node "https://nodejs.org"
  need pnpm "npm install -g pnpm"
  if [ ! -d web/node_modules ] || [ web/pnpm-lock.yaml -nt web/node_modules/.modules.yaml ]; then
    (cd web && pnpm install --silent)
  fi
  local stamp=web/dist/index.html
  if [ -f "$stamp" ] && [ -f web/dist/renderer/renderer.cjs ] &&
    [ -z "$(find web/src web/public web/index.html web/package.json web/pnpm-lock.yaml web/vite.config.ts web/tsconfig.json -newer "$stamp" -print -quit 2>/dev/null)" ]; then
    echo "前端没有改动，跳过构建"
    return
  fi
  (cd web && pnpm run build)
  # Vite 构建会清空 dist/；这个空文件让新 clone 的仓库不先构建前端也能编译 Go。
  touch web/dist/.gitkeep
}

build_go() {
  need go "https://go.dev/dl/"
  local version
  version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
  go build -ldflags "-s -w -X main.version=$version" -o "$BIN" ./cmd/davinci
  echo "已构建 ${BIN}（${version}）"
}

build() {
  build_web
  build_go
}

# 构建，并把 davinci 链接到 $GOPATH/bin。软链接而不是复制：davinci 顺着它找回
# 本仓库的 data/，以后重新构建即是更新。
install_cli() {
  build
  local gobin
  gobin="$(go env GOPATH)/bin"
  mkdir -p "$gobin"
  ln -sf "$PWD/$BIN" "$gobin/davinci"
  echo "已链接 $gobin/davinci → $PWD/$BIN"
  case ":$PATH:" in
    *":$gobin:"*) ;;
    *) echo "提示：$gobin 不在 PATH 里，把这行加进 ~/.zshrc 或 ~/.bashrc：export PATH=\"$gobin:\$PATH\"" ;;
  esac
}

case "${1:-}" in
  build)
    build
    ;;
  dev)
    need pnpm "npm install -g pnpm"
    [ -d web/node_modules ] || (cd web && pnpm install --silent)
    # Vite 把 /api /ws /assets 代理到 7789；那里已经有 davinci 在跑就直接用它。
    if curl -sf -m 2 http://127.0.0.1:7789/api/health >/dev/null; then
      echo "7789 上已有 davinci 服务，直接用它"
    else
      go run ./cmd/davinci serve &
      server=$!
      trap 'kill $server 2>/dev/null' EXIT INT TERM
    fi
    (cd web && pnpm run dev)
    ;;
  install)
    install_cli
    ;;
  skill)
    # skill 让 Agent 用 davinci 命令干活，所以先把命令装好。
    install_cli
    ./skills/install.sh
    ;;
  test)
    need go "https://go.dev/dl/"
    go vet ./...
    go test ./...
    (cd web && pnpm exec tsc --noEmit)
    ;;
  clean)
    rm -rf bin
    find web/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
    ;;
  -h | --help | help)
    sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'
    ;;
  "" | -*)
    build
    exec "$BIN" serve --open "$@"
    ;;
  *)
    echo "不认识的命令：${1}（./start.sh help 看用法）" >&2
    exit 1
    ;;
esac
