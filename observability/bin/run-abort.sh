#!/usr/bin/env bash
set -euo pipefail
umask 077

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
RUNS_DIR=${RUNS_DIR:-$ROOT_DIR/measurements}
ACTIVE_DIR=$RUNS_DIR/.active

if [[ $# -ne 1 ]] || [[ ! $1 =~ ^[A-Za-z0-9._-]+$ ]]; then
  echo "usage: $0 RUN_ID" >&2
  exit 2
fi

RUN_ID=$1
RUN_DIR=$RUNS_DIR/$RUN_ID
active_run=$(cat "$ACTIVE_DIR/run_id" 2>/dev/null || true)
if [[ $active_run != "$RUN_ID" ]]; then
  echo "Active measurement is '${active_run:-none}', not '$RUN_ID'." >&2
  exit 1
fi

sudo mysql -e "SET GLOBAL slow_query_log=OFF"
for pid_file in "$RUN_DIR"/*.pid; do
  [[ -f $pid_file ]] || continue
  read -r pid expected < "$pid_file" || continue
  if [[ $pid =~ ^[0-9]+$ && $expected =~ ^[0-9]+$ && -r /proc/$pid/stat ]] \
    && [[ $(awk '{print $22}' "/proc/$pid/stat" 2>/dev/null) == "$expected" ]]; then
    kill "$pid" 2>/dev/null || true
  fi
done
touch "$RUN_DIR/aborted"
rm -f "$ACTIVE_DIR/run_id" "$RUNS_DIR/current"
rmdir "$ACTIVE_DIR"
echo "Measurement aborted safely: $RUN_ID"
