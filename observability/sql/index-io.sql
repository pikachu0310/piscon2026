SELECT JSON_OBJECT(
  'schema_name', OBJECT_SCHEMA,
  'table_name', OBJECT_NAME,
  'index_name', COALESCE(INDEX_NAME, 'NO_INDEX'),
  'operations', COUNT_STAR,
  'total_seconds', ROUND(SUM_TIMER_WAIT / 1000000000000, 6),
  'read_operations', COUNT_READ,
  'write_operations', COUNT_WRITE,
  'fetch_operations', COUNT_FETCH
)
FROM performance_schema.table_io_waits_summary_by_index_usage
WHERE OBJECT_SCHEMA NOT IN (
  'mysql', 'sys', 'performance_schema', 'information_schema'
)
  AND COUNT_STAR > 0
ORDER BY SUM_TIMER_WAIT DESC;
