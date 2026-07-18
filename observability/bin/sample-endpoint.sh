#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 URL OUTPUT COUNT" >&2
  exit 2
fi

url=$1
output=$2
count=$3

for _ in $(seq 1 "$count"); do
  observed_at=$(date --iso-8601=ns)
  if payload=$(curl -fsS --max-time 2 "$url"); then
    jq -cn --arg observed_at "$observed_at" --argjson data "$payload" \
      '{observed_at: $observed_at, data: $data}' >> "$output"
  else
    jq -cn --arg observed_at "$observed_at" \
      '{observed_at: $observed_at, error: "endpoint unavailable"}' >> "$output"
  fi
  sleep 1
done
