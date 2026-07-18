#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
failed=0

check_command() {
  local command_name=$1
  if command -v "$command_name" >/dev/null; then
    printf 'ok   %-20s %s\n' "$command_name" "$(command -v "$command_name")"
  else
    printf 'FAIL %-20s missing\n' "$command_name"
    failed=1
  fi
}

for command_name in alp awk curl duckdb git ip jq mysql nginx nstat pidstat pt-query-digest sadf sar sha256sum ss; do
  check_command "$command_name"
done

if curl -fsS http://127.0.0.1:6060/debug/healthz | jq -e '.status == "ok"' >/dev/null; then
  echo "ok   diagnostics          loopback endpoint is ready"
else
  echo "FAIL diagnostics          http://127.0.0.1:6060 is not ready"
  failed=1
fi

performance_schema=$(sudo mysql --batch --raw --skip-column-names -e \
  "SELECT @@performance_schema" 2>/dev/null || true)
if [[ $performance_schema == "1" ]]; then
  echo "ok   performance_schema   enabled"
else
  echo "FAIL performance_schema   disabled"
  failed=1
fi

slow_log=$(sudo mysql --batch --raw --skip-column-names -e \
  "SELECT @@global.slow_query_log" 2>/dev/null || true)
if [[ $slow_log == "0" ]]; then
  echo "ok   slow_query_log       disabled for the default run"
else
  echo "FAIL slow_query_log       still enabled"
  failed=1
fi

digest_consumer=$(sudo mysql --batch --raw --skip-column-names -e \
  "SELECT ENABLED FROM performance_schema.setup_consumers WHERE NAME='statements_digest'" \
  2>/dev/null || true)
statement_instruments=$(sudo mysql --batch --raw --skip-column-names -e \
  "SELECT COUNT(*) FROM performance_schema.setup_instruments WHERE NAME LIKE 'statement/%' AND ENABLED='YES' AND TIMED='YES'" \
  2>/dev/null || true)
if [[ $digest_consumer == "YES" && $statement_instruments =~ ^[1-9][0-9]*$ ]]; then
  echo "ok   statement digests    consumer and timed instruments are enabled"
else
  echo "FAIL statement digests    consumer/instruments are not ready"
  failed=1
fi

if sudo -u mysql test -w /var/log/mysql/isucon-slow.log; then
  echo "ok   slow log file        writable by mysql"
else
  echo "FAIL slow log file        /var/log/mysql/isucon-slow.log is not writable by mysql"
  failed=1
fi

if sudo nginx -t >/dev/null 2>&1; then
  echo "ok   nginx                configuration is valid"
else
  echo "FAIL nginx                configuration test failed"
  failed=1
fi

if curl -fsS http://127.0.0.1/nginx_status | grep -q 'Active connections'; then
  echo "ok   nginx_status         loopback status endpoint is ready"
else
  echo "FAIL nginx_status         loopback status endpoint is not ready"
  failed=1
fi

# Exercise the real log path, not only nginx -t. The measurement start truncates
# this probe request after preflight.
curl -fsS -o /dev/null http://127.0.0.1/ || true
sleep 2
if sudo tail -n 1 /var/log/nginx/access.jsonl 2>/dev/null | jq -e \
  'type == "object" and (.request_time | type == "number")' >/dev/null; then
  echo "ok   access JSON          one valid request line was written"
else
  echo "FAIL access JSON          Nginx did not write valid JSON"
  failed=1
fi

if [[ -x "$ROOT_DIR/webapp/go/isucondition" ]]; then
  echo "ok   application          binary exists"
else
  echo "FAIL application          binary is missing"
  failed=1
fi

exit "$failed"
