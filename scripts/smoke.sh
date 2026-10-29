#!/usr/bin/env bash
# Contract smoke test for the echoheist dashboard.
#
# Verifies the security posture (token gate, no CORS, worker path immutable) and
# that every documented endpoint answers. Uses a deliberately invalid tab host so
# no request ever reaches the real site.
#
# Usage: ./scripts/smoke.sh [binary] [port]
set -uo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
BIN=${1:-./echoheistv2}
PORT=${2:-8791}
BASE="http://127.0.0.1:$PORT"
pass=0
fail=0
server_pid=""
# Keep the run hermetic: a private cache dir means the test never touches (or
# depends on) the user's real SQLite cache.
CACHE_HOME=${ECHOHEIST_SMOKE_CACHE:-$(mktemp -d)}

cleanup() {
  [ -n "$server_pid" ] && kill "$server_pid" 2>/dev/null
  wait "$server_pid" 2>/dev/null
  rm -rf "$CACHE_HOME"
}
trap cleanup EXIT

check() { # name expected actual
  if [ "$2" = "$3" ]; then
    printf '  \033[32mPASS\033[0m %-46s %s\n' "$1" "$3"; pass=$((pass+1))
  else
    printf '  \033[31mFAIL\033[0m %-46s want=%s got=%s\n' "$1" "$2" "$3"; fail=$((fail+1))
  fi
}

[ -x "$BIN" ] || { echo "binary not found or not executable: $BIN (run make build)" >&2; exit 2; }

LOG_FILE="$CACHE_HOME/server.log"
XDG_CACHE_HOME="$CACHE_HOME" "$BIN" --dashboard --listen "127.0.0.1:$PORT" \
  --worker ./download-worker.mjs --download-dir ./tabs --log-dir ./logs >"$LOG_FILE" 2>&1 &
server_pid=$!

for _ in $(seq 1 50); do
  curl -sf -o /dev/null "$BASE/" && break
  sleep 0.2
done

TOKEN=$(curl -s "$BASE/" | grep -o 'echoheist-token" content="[^"]*' | sed 's/.*content="//')
[ -n "$TOKEN" ] || { echo "could not extract token from served page" >&2; cat "$LOG_FILE"; exit 1; }

auth() { curl -s -o /dev/null -w '%{http_code}' -H "X-Echoheist-Token: $TOKEN" "$@"; }
authj() { curl -s -H "X-Echoheist-Token: $TOKEN" "$@"; }

echo "== security posture =="
check "GET /api/state without token" 401 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/state")"
check "GET /api/state with token" 200 "$(auth "$BASE/api/state")"
check "GET /api/health without token" 401 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/health")"
check "PUT /api/config without token" 401 "$(curl -s -o /dev/null -w '%{http_code}' -X PUT -d '{}' "$BASE/api/config")"
check "no CORS header on /api/state" "" "$(curl -s -D - -o /dev/null -H "Origin: https://evil.test" -H "X-Echoheist-Token: $TOKEN" "$BASE/api/state" | grep -ci access-control-allow-origin | sed 's/^0$//')"

orig_worker=$(authj "$BASE/api/config" | python3 -c 'import sys,json;print(json.load(sys.stdin)["workerPath"])' 2>/dev/null)
auth "$BASE/api/config" -X PUT -H 'Content-Type: application/json' -d '{"workerPath":"/tmp/evil.mjs"}' >/dev/null
check "workerPath not settable over HTTP" "$orig_worker" \
  "$(authj "$BASE/api/config" | python3 -c 'import sys,json;print(json.load(sys.stdin)["workerPath"])' 2>/dev/null)"

echo "== frontend serving (Vite bundle embedded via go:embed) =="
check "index.html carries the token meta tag" 1 "$(curl -s "$BASE/" | grep -c 'name="echoheist-token"')"
ASSET=$(curl -s "$BASE/" | grep -o '/dashboard/assets/[^"]*\.js' | head -1)
if [ -n "$ASSET" ]; then
  check "hashed asset served" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE$ASSET")"
  check "bundle sends the token header" 1 "$(curl -s "$BASE$ASSET" | grep -c 'X-Echoheist-Token')"
else
  printf '  \033[33mSKIP\033[0m asset check (index.html references no /dashboard/assets/*.js)\n'
fi
check "SPA fallback serves index.html" 1 "$(curl -s "$BASE/history" | grep -c 'name="echoheist-token"')"
check "stylesheet referenced" 1 "$(curl -s "$BASE/" | grep -c 'rel="stylesheet"')"

echo "== documented endpoints =="
for ep in /api/state /api/jobs /api/sources /api/history /api/favorites /api/logs /api/search?q= /api/artists /api/config /api/health; do
  check "GET $ep" 200 "$(auth "$BASE$ep")"
done

echo "== payload shapes (contract types) =="
authj "$BASE/api/state" | python3 "$SCRIPT_DIR/shape_check.py" state \
  | { read -r r; check "state payload types" shape-ok "$r"; }
authj "$BASE/api/jobs?since=0" | python3 "$SCRIPT_DIR/shape_check.py" jobs \
  | { read -r r; check "jobs payload types + log ordering" shape-ok "$r"; }
authj "$BASE/api/sources" | python3 "$SCRIPT_DIR/shape_check.py" sources \
  | { read -r r; check "sources payload + disabled reasons" shape-ok "$r"; }

echo "== job lifecycle (invalid host, no site traffic) =="
JOB=$(curl -s -o /dev/null -w '%{http_code}' -H "X-Echoheist-Token: $TOKEN" -X POST \
  -H 'Content-Type: application/json' \
  -d '{"tabs":[{"id":1,"song":"Smoke Test","artist":"Nobody","url":"https://example.test/tab/x-1"}]}' \
  "$BASE/api/jobs")
check "POST /api/jobs" 202 "$JOB"
JOB_ID=$(authj "$BASE/api/jobs" | python3 -c '
import sys,json
d=json.load(sys.stdin)
jobs=d.get("jobs") or []
print(jobs[0]["id"] if jobs else "")
' 2>/dev/null)
if [ -n "$JOB_ID" ]; then
  check "job is tracked" 1 "$(authj "$BASE/api/jobs" | python3 -c 'import sys,json;print(len(json.load(sys.stdin)["jobs"]))')"
  # The invalid-host job fails almost immediately, so cancelling it races the
  # worker. Both answers are correct: 200 when it was still cancelable, 409 when
  # it had already finished. 404 here would mean the route conflates "missing"
  # with "not cancelable".
  CANCEL_CODE=$(auth "$BASE/api/jobs/$JOB_ID" -X DELETE)
  case "$CANCEL_CODE" in
    200|409) printf '  \033[32mPASS\033[0m %-46s %s\n' "cancel, or 409 for an already-finished job" "$CANCEL_CODE"; pass=$((pass+1)) ;;
    *) printf '  \033[31mFAIL\033[0m %-46s want=200|409 got=%s\n' "cancel, or 409 for an already-finished job" "$CANCEL_CODE"; fail=$((fail+1)) ;;
  esac
  check "DELETE unknown job id is 404" 404 "$(auth "$BASE/api/jobs/no-such-job" -X DELETE)"
  authj "$BASE/api/jobs?since=0" | python3 -c 'import sys,json;d=json.load(sys.stdin);print("cursor-ok" if isinstance(d.get("cursor"),int) and "logs" in d else "bad")' \
    | { read -r r; check "incremental jobs cursor" cursor-ok "$r"; }
else
  printf '  \033[33mSKIP\033[0m job lifecycle (no job id returned)\n'
fi

echo "== favorites round-trip =="
auth "$BASE/api/favorites" -X POST -H 'Content-Type: application/json' \
  -d '{"tab":{"id":4242,"song":"Favorite Test","artist":"Nobody","url":"https://example.test/tab/f-4242"}}' >/dev/null
check "favorite persisted" 1 "$(authj "$BASE/api/favorites" | python3 -c 'import sys,json;print(sum(1 for t in json.load(sys.stdin)["items"] if t.get("id")==4242))' 2>/dev/null)"
check "DELETE /api/favorites/4242" 200 "$(auth "$BASE/api/favorites/4242" -X DELETE)"

echo
echo "passed=$pass failed=$fail"
[ "$fail" -eq 0 ] || { echo; echo "--- server log ---"; tail -30 "$LOG_FILE"; exit 1; }
