#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
exec "$SCRIPT_DIR/echoheistv2" \
  --worker "$PROJECT_DIR/download-worker.mjs" \
  --download-dir "$PROJECT_DIR/tabs" \
	--log-dir "$PROJECT_DIR/logs" \
  "$@"
