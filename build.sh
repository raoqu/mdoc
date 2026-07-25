#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

command -v go >/dev/null 2>&1 || { echo "错误：未找到 Go。" >&2; exit 1; }
command -v pnpm >/dev/null 2>&1 || { echo "错误：未找到 pnpm。" >&2; exit 1; }

if [[ ! -d node_modules ]]; then
  echo "首次构建，正在安装前端依赖…"
  pnpm install
fi

EMBED_DIR="$ROOT_DIR/cmd/mdocman/frontend_dist"

cleanup_embedded_frontend() {
  find "$EMBED_DIR" -mindepth 1 ! -name '.gitignore' -exec rm -rf {} +
}
trap cleanup_embedded_frontend EXIT
cleanup_embedded_frontend

echo "构建并预渲染 TypeScript 前端…"
pnpm exec vinext build --prerender-all

FRONTEND_INDEX="$ROOT_DIR/dist/server/prerendered-routes/index.html"
FRONTEND_CLIENT="$ROOT_DIR/dist/client"
if [[ ! -f "$FRONTEND_INDEX" || ! -d "$FRONTEND_CLIENT/assets" ]]; then
  echo "错误：前端构建未生成可内嵌的 index.html 或浏览器资源。" >&2
  exit 1
fi

echo "准备内嵌前端资源…"
cp -R "$FRONTEND_CLIENT/." "$EMBED_DIR/"
cp "$FRONTEND_INDEX" "$EMBED_DIR/index.html"

mkdir -p dist/bin

echo "构建内嵌前端的 Go 应用与发布端…"
go build -trimpath -ldflags="-s -w" -o dist/bin/mdoc ./cmd/mdocman
go build -trimpath -ldflags="-s -w" -o dist/bin/mdocman-site ./cmd/mdocman-site

echo "构建完成："
echo "  Mdoc 应用（已内嵌前端）：dist/bin/mdoc"
echo "  发布端：dist/bin/mdocman-site"
echo
echo "部署 Mdoc 时只需复制 dist/bin/mdoc。"
