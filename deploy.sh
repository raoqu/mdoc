#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

DEPLOY_TARGET="${DEPLOY_TARGET:-${1:-}}"
[[ -n "$DEPLOY_TARGET" ]] || { echo "用法：DEPLOY_TARGET=user@host:/var/www/notes ./deploy.sh" >&2; exit 1; }
command -v rsync >/dev/null 2>&1 || { echo "错误：需要 rsync。" >&2; exit 1; }

echo "增量部署 public-site/ → $DEPLOY_TARGET"
rsync -az --delete --checksum public-site/ "$DEPLOY_TARGET/"
echo "部署完成。"
