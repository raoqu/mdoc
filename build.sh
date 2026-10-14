#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

usage() {
  cat <<'EOF'
用法：./build.sh [cli|split|electron]

  cli       内嵌前端的独立可执行文件：./mdoc（默认）
  split     独立前端：dist/frontend/；Go 后端：dist/bin/mdocman
  electron  split 的别名，供 Electron 等桌面壳集成

两种模式均生成只读发布端：dist/bin/mdocman-site。
EOF
}

if (( $# > 1 )); then
  usage >&2
  exit 2
fi

BUILD_MODE="${1:-cli}"
case "$BUILD_MODE" in
  cli|split) ;;
  electron) BUILD_MODE=split ;;
  -h|--help) usage; exit 0 ;;
  *) echo "错误：未知构建模式：$BUILD_MODE" >&2; usage >&2; exit 2 ;;
esac

command -v go >/dev/null 2>&1 || { echo "错误：未找到 Go。" >&2; exit 1; }
command -v pnpm >/dev/null 2>&1 || { echo "错误：未找到 pnpm。" >&2; exit 1; }

if [[ ! -d node_modules ]]; then
  echo "首次构建，正在安装前端依赖…"
  pnpm install
fi

EMBED_DIR="$ROOT_DIR/cmd/mdocman/frontend_dist"

cleanup_embedded_frontend() {
  find "$EMBED_DIR" -mindepth 1 -maxdepth 1 ! -name '.gitignore' -exec rm -rf {} +
}
if [[ "$BUILD_MODE" == cli ]]; then
  mkdir -p "$EMBED_DIR"
  trap cleanup_embedded_frontend EXIT
  cleanup_embedded_frontend
fi

echo "构建并预渲染 TypeScript 前端…"
pnpm exec vinext build --prerender-all

FRONTEND_INDEX="$ROOT_DIR/dist/server/prerendered-routes/index.html"
FRONTEND_CLIENT="$ROOT_DIR/dist/client"
if [[ ! -f "$FRONTEND_INDEX" || ! -d "$FRONTEND_CLIENT/assets" ]]; then
  echo "错误：前端构建未生成可内嵌的 index.html 或浏览器资源。" >&2
  exit 1
fi

mkdir -p dist/bin

if [[ "$BUILD_MODE" == cli ]]; then
  echo "准备内嵌前端资源…"
  cp -R "$FRONTEND_CLIENT/." "$EMBED_DIR/"
  cp "$FRONTEND_INDEX" "$EMBED_DIR/index.html"

  echo "构建内嵌前端的 Go 应用…"
  go build -tags extended -trimpath -ldflags="-s -w" -o "$ROOT_DIR/mdoc" ./cmd/mdocman
else
  echo "准备独立前端资源…"
  rm -rf "$ROOT_DIR/dist/frontend"
  mkdir -p "$ROOT_DIR/dist/frontend"
  cp -R "$FRONTEND_CLIENT/." "$ROOT_DIR/dist/frontend/"
  cp "$FRONTEND_INDEX" "$ROOT_DIR/dist/frontend/index.html"

  echo "构建不内嵌前端的 Go 后端…"
  go build -tags extended,noembed -trimpath -ldflags="-s -w" -o dist/bin/mdocman ./cmd/mdocman
fi

echo "构建只读发布端…"
go build -tags extended -trimpath -ldflags="-s -w" -o dist/bin/mdocman-site ./cmd/mdocman-site

echo "构建完成："
if [[ "$BUILD_MODE" == cli ]]; then
  echo "  Mdoc 应用（已内嵌前端）：./mdoc"
else
  echo "  独立前端：dist/frontend/"
  echo "  Go 后端（未内嵌前端）：dist/bin/mdocman"
fi
echo "  发布端：dist/bin/mdocman-site"
echo
if [[ "$BUILD_MODE" == cli ]]; then
  echo "部署 Mdoc 时只需复制项目根目录中的 mdoc。"
else
  echo "Electron 等桌面壳可分别打包 dist/frontend/ 和 dist/bin/mdocman。"
  echo "前端需通过 HTTP 或自定义协议提供资源，并将后端路径代理到 Go API。"
fi

