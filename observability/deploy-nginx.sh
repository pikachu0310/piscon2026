#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
SITE=/etc/nginx/sites-available/isucondition.conf

sudo install -m 0644 \
  "$ROOT_DIR/observability/config/nginx/isucondition.conf" \
  "$SITE"
sudo nginx -t
sudo systemctl reload nginx

curl -fsS http://127.0.0.1/ >/dev/null
curl -fsS http://127.0.0.1/assets/favicon.d0f5f504.svg >/dev/null
curl -fsS http://127.0.0.1/nginx_status >/dev/null
echo "Nginx configuration is valid and static files are ready."
