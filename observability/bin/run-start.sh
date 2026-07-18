#!/usr/bin/env bash
set -euo pipefail
umask 077

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
RUNS_DIR=${RUNS_DIR:-$ROOT_DIR/measurements}
RUN_ID=${1:-$(date +%Y%m%d-%H%M%S)}
DURATION=${DURATION:-75}
MODE=${2:-standard}
ACTIVE_DIR=$RUNS_DIR/.active

write_pid_file() {
  local file=$1 pid=$2 start_ticks
  start_ticks=$(awk '{print $22}' "/proc/$pid/stat")
  printf '%s %s\n' "$pid" "$start_ticks" > "$file"
}

if [[ ! $RUN_ID =~ ^[A-Za-z0-9._-]+$ ]]; then
  echo "RUN_ID may contain only letters, numbers, dot, underscore, and hyphen." >&2
  exit 2
fi
if [[ ! $DURATION =~ ^[0-9]+$ ]] || (( DURATION < 10 || DURATION > 600 )); then
  echo "DURATION must be between 10 and 600 seconds." >&2
  exit 2
fi
if [[ $MODE != "standard" && $MODE != "slowlog" ]]; then
  echo "MODE must be standard or slowlog." >&2
  exit 2
fi

mkdir -p "$RUNS_DIR"

# Repair an interrupted slow-log run before preflight. The doctor then verifies
# that the low-overhead default state was actually restored.
if command -v mysql >/dev/null 2>&1; then
  sudo mysql -e "SET GLOBAL slow_query_log=OFF" >/dev/null 2>&1 || true
fi
doctor_output=$(mktemp)
if ! "$ROOT_DIR/observability/bin/doctor.sh" > "$doctor_output" 2>&1; then
  cat "$doctor_output" >&2
  rm -f "$doctor_output"
  echo "Preflight failed; no measurement directory was created." >&2
  exit 1
fi

if ! mkdir "$ACTIVE_DIR" 2>/dev/null; then
  active_run=$(cat "$ACTIVE_DIR/run_id" 2>/dev/null || echo unknown)
  echo "Another measurement is active: $active_run" >&2
  echo "Finish it, or use observability/bin/run-abort.sh $active_run." >&2
  rm -f "$doctor_output"
  exit 1
fi
echo "$RUN_ID" > "$ACTIVE_DIR/run_id"

RUN_DIR=$RUNS_DIR/$RUN_ID
if [[ -e $RUN_DIR ]]; then
  echo "Run already exists: $RUN_DIR" >&2
  rm -f "$doctor_output"
  rm -f "$ACTIVE_DIR/run_id"
  rmdir "$ACTIVE_DIR"
  exit 1
fi
mkdir -p "$RUN_DIR"
mv "$doctor_output" "$RUN_DIR/doctor.txt"

start_committed=false
cleanup_start_failure() {
  status=$?
  if [[ $start_committed != "true" ]]; then
    set +e
    sudo mysql -e "SET GLOBAL slow_query_log=OFF" >/dev/null 2>&1
    for pid_file in "$RUN_DIR"/*.pid; do
      [[ -f $pid_file ]] || continue
      read -r pid _ < "$pid_file"
      kill "$pid" 2>/dev/null
    done
    rm -f "$ACTIVE_DIR/run_id"
    rmdir "$ACTIVE_DIR" 2>/dev/null
    echo "Measurement start failed; samplers and slow log were stopped." >&2
  fi
  exit "$status"
}
trap cleanup_start_failure EXIT

start_epoch=$(date +%s)
start_time=$(date --iso-8601=seconds)
git_sha=$(git -C "$ROOT_DIR" rev-parse HEAD)
if git -C "$ROOT_DIR" diff --quiet --ignore-submodules HEAD --; then
  tracked_dirty=false
else
  tracked_dirty=true
fi

jq -n \
  --arg run_id "$RUN_ID" \
  --arg start_time "$start_time" \
  --argjson start_epoch "$start_epoch" \
  --arg git_sha "$git_sha" \
  --arg mode "$MODE" \
  --argjson duration "$DURATION" \
  --argjson tracked_dirty "$tracked_dirty" \
  --arg hostname "$(hostname)" \
  '{
    run_id: $run_id,
    start_time: $start_time,
    start_epoch: $start_epoch,
    git_sha: $git_sha,
    tracked_dirty: $tracked_dirty,
    mode: $mode,
    planned_duration_seconds: $duration,
    hostname: $hostname,
    score: null
  }' > "$RUN_DIR/meta.json"

{
  echo "kernel=$(uname -srmo)"
  echo "go=$(/home/isucon/local/go/bin/go version)"
  echo "mariadb=$(mysql --version)"
  echo "nginx=$(nginx -v 2>&1)"
  echo "alp=$(alp --version)"
  echo "duckdb=$(duckdb --version)"
  echo "pt_query_digest=$(pt-query-digest --version)"
  echo "sysstat=$(sar -V 2>&1 | head -1)"
  echo "jq=$(jq --version)"
} > "$RUN_DIR/versions.txt"

git -C "$ROOT_DIR" diff HEAD --binary > "$RUN_DIR/git.diff"
git -C "$ROOT_DIR" status --porcelain=v1 > "$RUN_DIR/git-status.txt"
git -C "$ROOT_DIR" ls-files --others --exclude-standard > "$RUN_DIR/untracked-files.txt"
sudo sha256sum \
  /etc/mysql/mariadb.conf.d/99-isucon-observability.cnf \
  /etc/nginx/conf.d/00-isucon-observability.conf \
  /etc/nginx/sites-available/isucondition.conf \
  /etc/systemd/system/isucondition.go.service \
  "$ROOT_DIR/webapp/go/isucondition" \
  > "$RUN_DIR/config.sha256"
sudo mysql --batch --raw > "$RUN_DIR/mysql-performance-schema-variables.tsv" \
  -e "SHOW VARIABLES LIKE 'performance_schema%'; SHOW STATUS LIKE 'Performance_schema_digest_lost'"
sudo mysql < "$ROOT_DIR/observability/sql/reset-performance-schema.sql"
sudo mysql --batch --raw > "$RUN_DIR/mysql-global-status.before.tsv" \
  -e "SHOW GLOBAL STATUS"
nstat -a -s -j > "$RUN_DIR/nstat.before.json"
ss -s > "$RUN_DIR/ss.before.txt"
ip -j -s link show > "$RUN_DIR/ip-link.before.json"
curl -fsS http://127.0.0.1/nginx_status > "$RUN_DIR/nginx-status.before.txt"
curl -fsS http://127.0.0.1:6060/debug/pprof/allocs \
  -o "$RUN_DIR/allocs.before.pb.gz"
curl -fsS http://127.0.0.1:6060/debug/runtime-metrics \
  -o "$RUN_DIR/runtime-metrics.before.json"
curl -fsS http://127.0.0.1:6060/debug/db-stats \
  -o "$RUN_DIR/db-stats.before.json"

sudo nginx -s reopen
sleep 1
sudo truncate -s 0 /var/log/nginx/access.jsonl

if [[ $MODE == "slowlog" ]]; then
  sudo mysql -e "SET GLOBAL slow_query_log=OFF; SET GLOBAL long_query_time=0; SET GLOBAL log_output='FILE'"
  sudo -u mysql truncate -s 0 /var/log/mysql/isucon-slow.log
  sudo mysql -e "SET GLOBAL slow_query_log=ON"
  touch "$RUN_DIR/slowlog.enabled"
fi

nohup sar -o "$RUN_DIR/sar.bin" 1 "$DURATION" \
  > "$RUN_DIR/sar.stdout.txt" 2>&1 < /dev/null &
write_pid_file "$RUN_DIR/sar.pid" $!

nohup sudo -n env LC_ALL=C S_TIME_FORMAT=ISO pidstat -h -u -r -d -w -p ALL 1 "$DURATION" \
  > "$RUN_DIR/pidstat.txt" 2>&1 < /dev/null &
write_pid_file "$RUN_DIR/pidstat.pid" $!

nohup "$ROOT_DIR/observability/bin/sample-endpoint.sh" \
  http://127.0.0.1:6060/debug/db-stats "$RUN_DIR/db-stats.jsonl" "$DURATION" \
  > "$RUN_DIR/db-stats.sampler.txt" 2>&1 < /dev/null &
write_pid_file "$RUN_DIR/db-stats.pid" $!

nohup curl -fsS --max-time "$((DURATION + 10))" \
  "http://127.0.0.1:6060/debug/pprof/profile?seconds=$DURATION" \
  -o "$RUN_DIR/cpu.pb.gz" > "$RUN_DIR/cpu.curl.txt" 2>&1 < /dev/null &
write_pid_file "$RUN_DIR/cpu.pid" $!

echo "$RUN_ID" > "$RUNS_DIR/current"
start_committed=true
echo "Measurement started: $RUN_ID ($MODE, ${DURATION}s)"
echo "Start the Portal benchmark now, then run:"
echo "  observability/bin/run-finish.sh $RUN_ID SCORE"
