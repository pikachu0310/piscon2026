#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
source_file=$ROOT_DIR/observability/config/nginx/isucondition-agent-kit.conf
target=/etc/nginx/sites-available/isucondition.conf

if [[ ! -e $target.before-agent-kit ]]; then
  sudo cp -a "$target" "$target.before-agent-kit"
fi
sudo install -m 0644 "$source_file" "$target"
sudo nginx -t
sudo systemctl reload nginx
curl -fsS --max-time 2 http://127.0.0.1/ -o /dev/null
echo "PISCON HTTP benchmark listener is ready."
