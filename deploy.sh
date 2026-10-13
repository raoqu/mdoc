#!/usr/bin/env bash
set -Eeuo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Never deploy the legacy public-site directory: always rebuild opted-in notes.
if (( $# > 1 )); then
  echo "用法：./deploy.sh [user@host:/www/wwwroot/mdoc/]" >&2
  exit 2
fi
if (( $# == 1 )); then export DEPLOY_TARGET="$1"; fi
exec "$ROOT_DIR/publish.sh" deploy
