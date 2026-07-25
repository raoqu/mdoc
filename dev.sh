#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

API_PORT="${API_PORT:-8080}"
export API_PORT

# Make the development contract explicit even when the caller's shell exports
# production-oriented environment variables. MDOC_DEV_HMR also enables Vite's
# polling watcher, which is more reliable for this workspace on macOS.
export NODE_ENV=development
export MDOC_DEV_HMR=1

cleanup() {
  local exit_code=$?
  trap - EXIT INT TERM
  if [[ -n "${BACKEND_PID:-}" ]] && kill -0 "$BACKEND_PID" 2>/dev/null; then
    kill "$BACKEND_PID" 2>/dev/null || true
    wait "$BACKEND_PID" 2>/dev/null || true
  fi
  exit "$exit_code"
}
trap cleanup EXIT INT TERM

command -v go >/dev/null 2>&1 || { echo "错误：未找到 Go。" >&2; exit 1; }
command -v pnpm >/dev/null 2>&1 || { echo "错误：未找到 pnpm。" >&2; exit 1; }

if [[ ! -d node_modules ]]; then
  echo "首次运行，正在安装前端依赖…"
  pnpm install
fi

if lsof -tiTCP:"$API_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "错误：后端端口 $API_PORT 已被占用。" >&2
  exit 1
fi

echo "启动 Go 后端：http://127.0.0.1:$API_PORT"
API_PORT="$API_PORT" go run ./cmd/mdocman &
BACKEND_PID=$!

for _ in {1..40}; do
  if curl --noproxy '*' --silent --fail "http://127.0.0.1:$API_PORT/api/notebooks" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$BACKEND_PID" 2>/dev/null; then
    echo "错误：Go 后端启动失败。" >&2
    wait "$BACKEND_PID"
    exit 1
  fi
  sleep 0.25
done

echo "启动 TypeScript 前端（HMR 已启用）；/api 与 /site 已自动代理到后端。"
pnpm run dev
