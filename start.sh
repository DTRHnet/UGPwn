#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec "$SCRIPT_DIR/echoheistv2" \
  --worker "$SCRIPT_DIR/download-worker.mjs" \
  --download-dir "$SCRIPT_DIR/tabs" \
	--log-dir "$SCRIPT_DIR/logs" \
  "$@"
