#!/usr/bin/env bash
set -euo pipefail
umask 077

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
RUNS_DIR=${RUNS_DIR:-$ROOT_DIR/measurements}
ACTIVE_DIR=$RUNS_DIR/.active

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 RUN_ID [SCORE]" >&2
  exit 2
fi

RUN_ID=$1
SCORE=${2:-null}
if [[ ! $RUN_ID =~ ^[A-Za-z0-9._-]+$ ]]; then
  echo "Invalid RUN_ID." >&2
  exit 2
fi
if [[ $SCORE != "null" && ! $SCORE =~ ^-?[0-9]+$ ]]; then
  echo "SCORE must be an integer or omitted." >&2
  exit 2
fi

RUN_DIR=$RUNS_DIR/$RUN_ID
if [[ ! -f $RUN_DIR/meta.json ]]; then
  echo "Run does not exist: $RUN_DIR" >&2
  exit 1
fi
if [[ -f $RUN_DIR/finished ]]; then
  echo "Run is already finished: $RUN_ID" >&2
  exit 1
fi
active_run=$(cat "$ACTIVE_DIR/run_id" 2>/dev/null || true)
if [[ $active_run != "$RUN_ID" ]]; then
  echo "Active measurement is '${active_run:-none}', not '$RUN_ID'." >&2
  exit 1
fi

ERRORS=$RUN_DIR/errors.txt
: > "$ERRORS"
finalized=false

record_error() {
  echo "$*" | tee -a "$ERRORS" >&2
}

pid_file_matches() {
  local pid_file=$1 pid expected actual
  read -r pid expected < "$pid_file" || return 1
  [[ $pid =~ ^[0-9]+$ && $expected =~ ^[0-9]+$ && -r /proc/$pid/stat ]] || return 1
  actual=$(awk '{print $22}' "/proc/$pid/stat" 2>/dev/null) || return 1
  [[ $actual == "$expected" ]]
}

stop_samplers() {
  local deadline pid pid_file any_running
  deadline=$(( $(date +%s) + 20 ))
  while (( $(date +%s) < deadline )); do
    any_running=false
    for pid_file in "$RUN_DIR"/*.pid; do
      [[ -f $pid_file ]] || continue
      read -r pid _ < "$pid_file"
      if pid_file_matches "$pid_file" && kill -0 "$pid" 2>/dev/null; then
        any_running=true
        break
      fi
    done
    [[ $any_running == true ]] || return 0
    sleep 1
  done

  for pid_file in "$RUN_DIR"/*.pid; do
    [[ -f $pid_file ]] || continue
    read -r pid _ < "$pid_file"
    if pid_file_matches "$pid_file" && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      record_error "collector timed out and was stopped: $(basename "$pid_file")"
    fi
  done
}

cleanup_finish() {
  status=$?
  set +e
  sudo mysql -e "SET GLOBAL slow_query_log=OFF" >/dev/null 2>&1
  stop_samplers
  if [[ $finalized == true ]]; then
    rm -f "$ACTIVE_DIR/run_id"
    rmdir "$ACTIVE_DIR" 2>/dev/null
    if [[ -f $RUNS_DIR/current ]] && [[ $(cat "$RUNS_DIR/current") == "$RUN_ID" ]]; then
      rm -f "$RUNS_DIR/current"
    fi
  else
    echo "Finish was interrupted. Raw files were kept; rerun run-finish.sh or use run-abort.sh." >&2
  fi
  exit "$status"
}
trap cleanup_finish EXIT

# Always return the database to the low-overhead default, even when this was a
# standard run. This also makes finish safe after a partially failed start.
sudo mysql -e "SET GLOBAL slow_query_log=OFF"
end_epoch=$(date +%s)
end_time=$(date --iso-8601=seconds)

if [[ ! -f $RUN_DIR/access.jsonl ]]; then
  if sudo mv /var/log/nginx/access.jsonl "$RUN_DIR/access.jsonl"; then
    sudo chown "$(id -un):$(id -gn)" "$RUN_DIR/access.jsonl"
    sudo chmod 0600 "$RUN_DIR/access.jsonl"
    if ! sudo nginx -s reopen; then
      record_error "nginx log reopen failed after rotation"
    fi
  else
    record_error "Nginx access log rotation failed"
  fi
fi

if ! sudo mysql --batch --raw --skip-column-names \
  < "$ROOT_DIR/observability/sql/statement-digests.sql" \
  > "$RUN_DIR/mysql-statement-digests.ndjson"; then
  record_error "Performance Schema statement digest capture failed"
fi
if ! sudo mysql --batch --raw --skip-column-names \
  < "$ROOT_DIR/observability/sql/table-io.sql" \
  > "$RUN_DIR/mysql-table-io.ndjson"; then
  record_error "Performance Schema table I/O capture failed"
fi
if ! sudo mysql --batch --raw --skip-column-names \
  < "$ROOT_DIR/observability/sql/index-io.sql" \
  > "$RUN_DIR/mysql-index-io.ndjson"; then
  record_error "Performance Schema index I/O capture failed"
fi
if ! sudo mysql --batch --raw > "$RUN_DIR/mysql-global-status.after.tsv" \
  -e "SHOW GLOBAL STATUS"; then
  record_error "MariaDB global status capture failed"
fi

awk -F '\t' '
  NR == FNR && FNR > 1 { before[$1] = $2; next }
  FNR > 1 && ($2 ~ /^[0-9]+([.][0-9]+)?$/) && (before[$1] ~ /^[0-9]+([.][0-9]+)?$/) {
    printf "{\"variable\":\"%s\",\"before\":%s,\"after\":%s,\"delta\":%.6f}\n", $1, before[$1], $2, $2 - before[$1]
  }
' "$RUN_DIR/mysql-global-status.before.tsv" "$RUN_DIR/mysql-global-status.after.tsv" \
  > "$RUN_DIR/mysql-global-status.delta.ndjson" || record_error "MariaDB status delta failed"

nstat -a -s -j > "$RUN_DIR/nstat.after.json" || record_error "nstat capture failed"
ss -s > "$RUN_DIR/ss.after.txt" || record_error "ss capture failed"
ip -j -s link show > "$RUN_DIR/ip-link.after.json" || record_error "network link capture failed"
curl -fsS http://127.0.0.1/nginx_status > "$RUN_DIR/nginx-status.after.txt" \
  || record_error "Nginx status capture failed"

# The run boundary above is now fixed. Waiting here lets fixed-duration files
# finish without allowing later traffic into the access/P_S/network snapshots.
stop_samplers

sadf -j "$RUN_DIR/sar.bin" -- -A > "$RUN_DIR/sar.json" \
  2> "$RUN_DIR/sadf.stderr.txt" || record_error "sadf JSON conversion failed"
{
  echo "=== CPU average ==="
  sar -u -f "$RUN_DIR/sar.bin" | grep '^Average:' || true
  echo "=== run queue average ==="
  sar -q -f "$RUN_DIR/sar.bin" | grep '^Average:' || true
  echo "=== memory average ==="
  sar -r -f "$RUN_DIR/sar.bin" | grep '^Average:' || true
  echo "=== block devices average ==="
  sar -d -p -f "$RUN_DIR/sar.bin" | grep '^Average:' || true
  echo "=== network devices average ==="
  sar -n DEV -f "$RUN_DIR/sar.bin" | grep '^Average:' || true
} > "$RUN_DIR/os-summary.txt"

curl -fsS 'http://127.0.0.1:6060/debug/pprof/heap?gc=1' \
  -o "$RUN_DIR/heap.pb.gz" || record_error "heap profile capture failed"
curl -fsS http://127.0.0.1:6060/debug/pprof/allocs \
  -o "$RUN_DIR/allocs.after.pb.gz" || record_error "allocation profile capture failed"
curl -fsS 'http://127.0.0.1:6060/debug/pprof/goroutine?debug=1' \
  -o "$RUN_DIR/goroutine.txt" || record_error "goroutine capture failed"
curl -fsS http://127.0.0.1:6060/debug/runtime-metrics \
  -o "$RUN_DIR/runtime-metrics.after.json" || record_error "runtime metrics capture failed"
curl -fsS http://127.0.0.1:6060/debug/db-stats \
  -o "$RUN_DIR/db-stats.after.json" || record_error "DBStats capture failed"

if ! jq -n \
  --slurpfile before "$RUN_DIR/runtime-metrics.before.json" \
  --slurpfile after "$RUN_DIR/runtime-metrics.after.json" '
    ($before[0] | map({key: .name, value: .}) | from_entries) as $b
    | [$after[0][]
       | select((.kind == "uint64" or .kind == "float64") and ($b[.name] != null))
       | {name, kind, before: $b[.name].value, after: .value,
          delta: (.value - $b[.name].value)}]
  ' > "$RUN_DIR/runtime-metrics.delta.json"; then
  record_error "runtime metrics delta failed"
fi
if ! jq -n \
  --slurpfile before "$RUN_DIR/db-stats.before.json" \
  --slurpfile after "$RUN_DIR/db-stats.after.json" '
    $before[0] as $b | $after[0] as $a
    | $a + {
        wait_count_delta: ($a.wait_count - $b.wait_count),
        wait_duration_seconds_delta: ($a.wait_duration_seconds - $b.wait_duration_seconds),
        max_idle_closed_delta: ($a.max_idle_closed - $b.max_idle_closed),
        max_idle_time_closed_delta: ($a.max_idle_time_closed - $b.max_idle_time_closed),
        max_lifetime_closed_delta: ($a.max_lifetime_closed - $b.max_lifetime_closed)
      }
  ' > "$RUN_DIR/db-stats.delta.json"; then
  record_error "DBStats delta failed"
fi

GO_BIN=/home/isucon/local/go/bin/go
APP_BIN=$ROOT_DIR/webapp/go/isucondition
if [[ -s $RUN_DIR/cpu.pb.gz ]]; then
  "$GO_BIN" tool pprof -top -nodecount=40 "$APP_BIN" "$RUN_DIR/cpu.pb.gz" \
    > "$RUN_DIR/cpu.top.txt" 2> "$RUN_DIR/cpu.top.stderr.txt" \
    || record_error "CPU pprof top failed"
  "$GO_BIN" tool pprof -top -cum -nodecount=40 "$APP_BIN" "$RUN_DIR/cpu.pb.gz" \
    > "$RUN_DIR/cpu.cum.txt" 2> "$RUN_DIR/cpu.cum.stderr.txt" \
    || record_error "CPU pprof cumulative top failed"
  "$GO_BIN" tool pprof -tags "$APP_BIN" "$RUN_DIR/cpu.pb.gz" \
    > "$RUN_DIR/cpu.tags.txt" 2> "$RUN_DIR/cpu.tags.stderr.txt" \
    || record_error "CPU pprof tags failed"
  "$GO_BIN" tool pprof -svg "$APP_BIN" "$RUN_DIR/cpu.pb.gz" \
    > "$RUN_DIR/cpu.svg" 2> "$RUN_DIR/cpu.svg.stderr.txt" \
    || record_error "CPU pprof SVG failed"
else
  record_error "CPU profile is empty or missing"
fi
if [[ -s $RUN_DIR/heap.pb.gz ]]; then
  "$GO_BIN" tool pprof -top -inuse_space -nodecount=40 "$APP_BIN" "$RUN_DIR/heap.pb.gz" \
    > "$RUN_DIR/heap.top.txt" 2> "$RUN_DIR/heap.top.stderr.txt" \
    || record_error "heap pprof top failed"
fi
if [[ -s $RUN_DIR/allocs.before.pb.gz && -s $RUN_DIR/allocs.after.pb.gz ]]; then
  "$GO_BIN" tool pprof -top -alloc_space -nodecount=40 \
    -base "$RUN_DIR/allocs.before.pb.gz" "$APP_BIN" "$RUN_DIR/allocs.after.pb.gz" \
    > "$RUN_DIR/allocs.delta.top.txt" 2> "$RUN_DIR/allocs.delta.top.stderr.txt" \
    || record_error "allocation pprof delta failed"
fi

if [[ -s $RUN_DIR/access.jsonl ]]; then
  alp json --file "$RUN_DIR/access.jsonl" \
    --config "$ROOT_DIR/observability/analysis/alp.yml" \
    --dump "$RUN_DIR/alp.yaml" > "$RUN_DIR/alp.csv" \
    2> "$RUN_DIR/alp.stderr.txt" || record_error "alp analysis failed"
  {
    printf "SET VARIABLE access_log = '%s';\n" "$RUN_DIR/access.jsonl"
    cat "$ROOT_DIR/observability/analysis/access-summary.sql"
  } | duckdb -json > "$RUN_DIR/access-summary.json" \
    2> "$RUN_DIR/duckdb.stderr.txt" || record_error "DuckDB access analysis failed"
else
  record_error "Nginx access log is empty or missing"
fi

if [[ -f $RUN_DIR/slowlog.enabled ]]; then
  if sudo install -o "$(id -un)" -g "$(id -gn)" -m 0600 \
    /var/log/mysql/isucon-slow.log "$RUN_DIR/slow.private.log"; then
    pt-query-digest --no-version-check --limit=100 \
      "$RUN_DIR/slow.private.log" > "$RUN_DIR/pt-query-digest.txt" \
      2> "$RUN_DIR/pt-query-digest.stderr.txt" \
      || record_error "pt-query-digest text analysis failed"
    if ! pt-query-digest --no-version-check --limit=100 --output=json-anon \
      "$RUN_DIR/slow.private.log" | jq -s . \
      > "$RUN_DIR/pt-query-digest.json"; then
      rm -f "$RUN_DIR/pt-query-digest.json"
      record_error "pt-query-digest anonymous JSON analysis failed"
    fi
  else
    record_error "slow query log capture failed"
  fi
fi

error_count=$(grep -c . "$ERRORS" || true)
jq \
  --arg end_time "$end_time" \
  --argjson end_epoch "$end_epoch" \
  --argjson score "$SCORE" \
  --argjson error_count "$error_count" \
  '. + {end_time: $end_time, end_epoch: $end_epoch, score: $score,
        capture_error_count: $error_count}' \
  "$RUN_DIR/meta.json" > "$RUN_DIR/meta.json.tmp"
mv "$RUN_DIR/meta.json.tmp" "$RUN_DIR/meta.json"

if (( error_count > 0 )); then
  touch "$RUN_DIR/finished-with-errors"
fi
find "$RUN_DIR" -type f ! -name manifest.sha256 -printf '%P\0' \
  | sort -z \
  | while IFS= read -r -d '' path; do
      sha256sum "$RUN_DIR/$path"
    done > "$RUN_DIR/manifest.sha256"

touch "$RUN_DIR/finished"
finalized=true
echo "Measurement finished: $RUN_DIR (capture errors: $error_count)"
echo "Read these first: meta.json, mysql-statement-digests.ndjson, alp.csv, cpu.top.txt, os-summary.txt"
