#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

command -v go >/dev/null 2>&1 || { echo "错误：未找到 Go。" >&2; exit 1; }
command -v pnpm >/dev/null 2>&1 || { echo "错误：未找到 pnpm。" >&2; exit 1; }

if [[ ! -d node_modules ]]; then
  pnpm install
fi

mkdir -p dist/bin

echo "构建 TypeScript 前端…"
pnpm run build

echo "构建 Go 管理端与发布端…"
go build -trimpath -ldflags="-s -w" -o dist/bin/mdocman-admin ./cmd/mdocman
go build -trimpath -ldflags="-s -w" -o dist/bin/mdocman-site ./cmd/mdocman-site

echo "构建完成："
echo "  前端：dist/"
echo "  管理端：dist/bin/mdocman-admin"
echo "  发布端：dist/bin/mdocman-site"
