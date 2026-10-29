#!/usr/bin/env bash
# Contract smoke test for the echoheist dashboard.
#
# Verifies the security posture (token gate, no CORS, worker path immutable) and
# that every documented endpoint answers. Uses a deliberately invalid tab host so
# no request ever reaches the real site.
#
# Usage: ./scripts/smoke.sh [binary] [port]
set -uo pipefail

BIN=${1:-./echoheistv2}
PORT=${2:-8791}
BASE="http://127.0.0.1:$PORT"
pass=0
fail=0
server_pid=""

cleanup() { [ -n "$server_pid" ] && kill "$server_pid" 2>/dev/null; wait "$server_pid" 2>/dev/null; }
trap cleanup EXIT

check() { # name expected actual
  if [ "$2" = "$3" ]; then
    printf '  \033[32mPASS\033[0m %-46s %s\n' "$1" "$3"; pass=$((pass+1))
  else
    printf '  \033[31mFAIL\033[0m %-46s want=%s got=%s\n' "$1" "$2" "$3"; fail=$((fail+1))
  fi
}

[ -x "$BIN" ] || { echo "binary not found or not executable: $BIN (run make build)" >&2; exit 2; }

"$BIN" --dashboard --listen "127.0.0.1:$PORT" \
  --worker ./download-worker.mjs --download-dir ./tabs --log-dir ./logs >/tmp/eh-smoke.log 2>&1 &
server_pid=$!

for _ in $(seq 1 50); do
  curl -sf -o /dev/null "$BASE/" && break
  sleep 0.2
done

TOKEN=$(curl -s "$BASE/" | grep -o 'echoheist-token" content="[^"]*' | sed 's/.*content="//')
[ -n "$TOKEN" ] || { echo "could not extract token from served page" >&2; cat /tmp/eh-smoke.log; exit 1; }

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

echo "== documented endpoints =="
for ep in /api/state /api/jobs /api/sources /api/history /api/favorites /api/logs /api/search?q= /api/artists /api/config /api/health; do
  check "GET $ep" 200 "$(auth "$BASE$ep")"
done

echo "== state shape =="
authj "$BASE/api/state" | python3 -c '
import sys,json
s=json.load(sys.stdin)
need=["version","config","jobs","counts","metrics","sources","options"]
missing=[k for k in need if k not in s]
print("contract-ok" if not missing else "missing:"+",".join(missing))
' | { read -r r; check "state keys per contract" contract-ok "$r"; }

echo "== sources honesty =="
authj "$BASE/api/sources" | python3 -c '
import sys,json
items=json.load(sys.stdin)["items"]
ug=[s for s in items if s.get("id")=="ultimate-guitar"]
bad=[s for s in items if not s.get("enabled") and not s.get("reason")]
print("ok" if ug and ug[0].get("enabled") and not bad else "bad")
' | { read -r r; check "ultimate-guitar enabled, disabled entries carry reasons" ok "$r"; }

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
  check "DELETE /api/jobs/{id}" 200 "$(auth "$BASE/api/jobs/$JOB_ID" -X DELETE)"
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
[ "$fail" -eq 0 ] || { echo; echo "--- server log ---"; tail -30 /tmp/eh-smoke.log; exit 1; }
