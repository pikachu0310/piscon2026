#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
GO_BIN=${GO_BIN:-/home/isucon/local/go/bin/go}

cd "$ROOT_DIR/webapp/go"
"$GO_BIN" test ./...
"$GO_BIN" build -o isucondition .
sudo systemctl restart isucondition.go

for _ in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:6060/debug/healthz >/dev/null; then
    echo "Application and loopback diagnostics endpoint are ready."
    exit 0
  fi
  sleep 1
done

echo "Diagnostics endpoint did not become ready." >&2
sudo systemctl status isucondition.go --no-pager >&2 || true
exit 1
