#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)

sudo install -m 0644 \
  "$ROOT_DIR/observability/config/mysql/99-isucon-observability.cnf" \
  /etc/mysql/mariadb.conf.d/99-isucon-observability.cnf
sudo systemctl restart mariadb
sudo mysql < "$ROOT_DIR/observability/sql/enable-performance-schema.sql"
sudo mysql --batch --raw --skip-column-names -e \
  "SELECT CONCAT('innodb_flush_log_at_trx_commit=', @@innodb_flush_log_at_trx_commit)"
