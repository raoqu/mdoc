#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

echo "清理构建产物和开发缓存…"
rm -rf dist .next .vinext .wrangler public-site
rm -f tsconfig.tsbuildinfo
go clean -cache
echo "清理完成。依赖目录 node_modules 和笔记数据 data/ 已保留。"
