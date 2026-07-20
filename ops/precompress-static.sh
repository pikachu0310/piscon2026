#!/usr/bin/env bash
set -euo pipefail

public_dir=${1:-/home/isucon/webapp/public}

if [[ ! -d $public_dir/assets ]]; then
  echo "assets directory not found: $public_dir/assets" >&2
  exit 1
fi

find "$public_dir/assets" -type f \
  \( -name '*.js' -o -name '*.css' -o -name '*.svg' -o -name '*.json' \) \
  ! -name '*.gz' -print0 \
  | xargs -0 -r -n 1 gzip -9 -k -f

find "$public_dir/assets" -type f -name '*.gz' -printf '%p %s bytes\n' \
  | sort
