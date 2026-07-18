#!/usr/bin/env bash
set -euo pipefail
umask 077

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
RUNS_DIR=${RUNS_DIR:-$ROOT_DIR/measurements}
ACTIVE_DIR=$RUNS_DIR/.active

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 RUN_ID [SECONDS]" >&2
  exit 2
fi

RUN_ID=$1
DURATION=${2:-10}
if [[ ! $RUN_ID =~ ^[A-Za-z0-9._-]+$ ]] \
  || [[ ! $DURATION =~ ^[0-9]+$ ]] \
  || (( DURATION < 1 || DURATION > 30 )); then
  echo "RUN_ID is invalid or SECONDS is outside 1..30." >&2
  exit 2
fi

mkdir -p "$RUNS_DIR"
if ! mkdir "$ACTIVE_DIR" 2>/dev/null; then
  echo "Another measurement is active: $(cat "$ACTIVE_DIR/run_id" 2>/dev/null || echo unknown)" >&2
  exit 1
fi
echo "$RUN_ID" > "$ACTIVE_DIR/run_id"
locked=true
cleanup() {
  status=$?
  if [[ $locked == true ]]; then
    rm -f "$ACTIVE_DIR/run_id"
    rmdir "$ACTIVE_DIR" 2>/dev/null || true
  fi
  exit "$status"
}
trap cleanup EXIT

RUN_DIR=$RUNS_DIR/$RUN_ID
if [[ -e $RUN_DIR ]]; then
  echo "Run already exists: $RUN_DIR" >&2
  exit 1
fi
mkdir "$RUN_DIR"

curl -fsS "http://127.0.0.1:6060/debug/pprof/trace?seconds=$DURATION" \
  -o "$RUN_DIR/trace.out"

GO_BIN=/home/isucon/local/go/bin/go
for profile_type in net sync syscall sched; do
  "$GO_BIN" tool trace -pprof="$profile_type" "$RUN_DIR/trace.out" \
    > "$RUN_DIR/trace-$profile_type.pb.gz"
  "$GO_BIN" tool pprof -top -nodecount=40 "$RUN_DIR/trace-$profile_type.pb.gz" \
    > "$RUN_DIR/trace-$profile_type.top.txt"
done

jq -n \
  --arg run_id "$RUN_ID" \
  --arg time "$(date --iso-8601=seconds)" \
  --arg git_sha "$(git -C "$ROOT_DIR" rev-parse HEAD)" \
  --argjson duration "$DURATION" \
  '{run_id: $run_id, type: "runtime-trace", time: $time,
    git_sha: $git_sha, duration_seconds: $duration}' > "$RUN_DIR/meta.json"
find "$RUN_DIR" -type f ! -name manifest.sha256 -printf '%P\0' \
  | sort -z \
  | while IFS= read -r -d '' path; do sha256sum "$RUN_DIR/$path"; done \
  > "$RUN_DIR/manifest.sha256"
touch "$RUN_DIR/finished"

echo "Trace saved under $RUN_DIR"
