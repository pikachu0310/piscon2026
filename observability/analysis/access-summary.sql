WITH logs AS (
  SELECT
    method,
    regexp_replace(
      split_part(uri, '?', 1),
      '/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}',
      '/:uuid',
      'g'
    ) AS route,
    try_cast(status AS INTEGER) AS status,
    try_cast(body_bytes AS BIGINT) AS body_bytes,
    try_cast(request_time AS DOUBLE) AS request_time,
    try_cast(nullif(response_time, '-') AS DOUBLE) AS upstream_time,
    try_cast(nullif(upstream_connect_time, '-') AS DOUBLE) AS connect_time,
    try_cast(nullif(upstream_header_time, '-') AS DOUBLE) AS header_time,
    connection_requests
  FROM read_ndjson_auto(getvariable('access_log'))
)
SELECT
  method,
  route,
  count(*) AS requests,
  count(*) FILTER (WHERE status >= 500) AS errors_5xx,
  round(sum(request_time), 6) AS total_seconds,
  round(avg(request_time) * 1000, 3) AS avg_ms,
  round(quantile_cont(request_time, 0.95) * 1000, 3) AS p95_ms,
  round(quantile_cont(request_time, 0.99) * 1000, 3) AS p99_ms,
  round(max(request_time) * 1000, 3) AS max_ms,
  round(sum(request_time - coalesce(upstream_time, request_time)), 6) AS nginx_side_seconds,
  round(avg(connect_time) * 1000, 3) AS avg_connect_ms,
  round(avg(header_time) * 1000, 3) AS avg_header_ms,
  sum(body_bytes) AS response_bytes,
  round(avg(connection_requests), 3) AS avg_requests_per_connection
FROM logs
GROUP BY ALL
ORDER BY total_seconds DESC
LIMIT 100;
