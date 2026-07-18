#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
INITIAL_NGINX_SITE_SHA256=0ae230d43936500205480ffab19a8d8d578d02c8bb50bcb0aed344fb54ffc658
ALP_VERSION=1.0.21
ALP_SHA256=890628b2fe5307b637e4f875fd854a737f787d9ebc73d5f36554cfeaa7acd00d
DUCKDB_VERSION=1.5.4
DUCKDB_SHA256=c1d6db2294895c97849bee574b21eb462528857ccf8a23617dd3e0a05dd4b770

if [[ $(uname -m) != "x86_64" ]]; then
  echo "This installer is pinned for the PISCON x86_64 image." >&2
  exit 1
fi

sudo apt-get update
sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
  ca-certificates curl graphviz iproute2 jq percona-toolkit sysstat

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

alp_archive="$tmp_dir/alp.tar.gz"
curl -fsSL \
  "https://github.com/tkuchiki/alp/releases/download/v${ALP_VERSION}/alp_linux_amd64.tar.gz" \
  -o "$alp_archive"
echo "${ALP_SHA256}  ${alp_archive}" | sha256sum -c -
tar -xzf "$alp_archive" -C "$tmp_dir"
sudo install -m 0755 "$tmp_dir/alp" /usr/local/bin/alp

duckdb_archive="$tmp_dir/duckdb.gz"
curl -fsSL \
  "https://github.com/duckdb/duckdb/releases/download/v${DUCKDB_VERSION}/duckdb_cli-linux-amd64.gz" \
  -o "$duckdb_archive"
echo "${DUCKDB_SHA256}  ${duckdb_archive}" | sha256sum -c -
gzip -dc "$duckdb_archive" > "$tmp_dir/duckdb"
sudo install -m 0755 "$tmp_dir/duckdb" /usr/local/bin/duckdb

sudo install -m 0644 \
  "$ROOT_DIR/observability/config/mysql/99-isucon-observability.cnf" \
  /etc/mysql/mariadb.conf.d/99-isucon-observability.cnf
sudo install -m 0644 \
  "$ROOT_DIR/observability/config/nginx/00-isucon-observability.conf" \
  /etc/nginx/conf.d/00-isucon-observability.conf
nginx_site=/etc/nginx/sites-available/isucondition.conf
desired_site=$ROOT_DIR/observability/config/nginx/isucondition.conf
if ! sudo cmp -s "$desired_site" "$nginx_site"; then
  current_site_sha=$(sudo sha256sum "$nginx_site" | awk '{print $1}')
  if [[ $current_site_sha != "$INITIAL_NGINX_SITE_SHA256" ]]; then
    echo "Refusing to overwrite a customized Nginx site: $nginx_site" >&2
    echo "Current SHA-256: $current_site_sha" >&2
    echo "Merge access_log and /nginx_status from $desired_site manually." >&2
    exit 1
  fi
  if [[ ! -e $nginx_site.before-observability ]]; then
    sudo cp -a "$nginx_site" "$nginx_site.before-observability"
  fi
  sudo install -m 0644 "$desired_site" "$nginx_site"
fi

# Never create the diagnostic log as root: mysqld must append to it after a
# slowlog run truncates the file.
if [[ ! -e /var/log/mysql/isucon-slow.log ]]; then
  sudo install -o mysql -g adm -m 0640 /dev/null /var/log/mysql/isucon-slow.log
else
  sudo chown mysql:adm /var/log/mysql/isucon-slow.log
  sudo chmod 0640 /var/log/mysql/isucon-slow.log
fi

sudo nginx -t
sudo systemctl restart mariadb
sudo mysql < "$ROOT_DIR/observability/sql/enable-performance-schema.sql"
sudo systemctl reload nginx

echo "Installed observability tools:"
alp --version
duckdb --version
pt-query-digest --version
sar -V 2>&1 | head -1
jq --version
sudo mysql --batch --raw --skip-column-names -e \
  "SELECT CONCAT('performance_schema=', @@performance_schema)"
