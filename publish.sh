#!/usr/bin/env bash
set -Eeuo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"
# Specify MDOC_BIN for an installed binary; the repository defaults to its source.
if [[ -n "${MDOC_BIN:-}" ]]; then
  exec "$MDOC_BIN" publish "$@"
fi
exec go run -tags extended ./cmd/mdocman publish "$@"
