#!/bin/sh
set -eu

assets_dir=${1:-/home/isucon/webapp/public/assets}

if [ ! -d "$assets_dir" ]; then
  echo "assets directory does not exist: $assets_dir" >&2
  exit 1
fi

# Nginx's gzip_static module serves these sidecars only when the client advertises
# gzip support. Keep the originals for clients without it. -n makes the output
# reproducible by omitting source names and timestamps from the gzip header.
find "$assets_dir" -maxdepth 1 -type f \( -name '*.js' -o -name '*.css' \) -exec gzip -n -6 -k -f -- '{}' \;
