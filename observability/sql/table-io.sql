SELECT JSON_OBJECT(
  'schema_name', OBJECT_SCHEMA,
  'table_name', OBJECT_NAME,
  'operations', COUNT_STAR,
  'total_seconds', ROUND(SUM_TIMER_WAIT / 1000000000000, 6),
  'read_operations', COUNT_READ,
  'read_seconds', ROUND(SUM_TIMER_READ / 1000000000000, 6),
  'write_operations', COUNT_WRITE,
  'write_seconds', ROUND(SUM_TIMER_WRITE / 1000000000000, 6),
  'fetch_operations', COUNT_FETCH,
  'insert_operations', COUNT_INSERT,
  'update_operations', COUNT_UPDATE,
  'delete_operations', COUNT_DELETE
)
FROM performance_schema.table_io_waits_summary_by_table
WHERE OBJECT_SCHEMA NOT IN (
  'mysql', 'sys', 'performance_schema', 'information_schema'
)
  AND COUNT_STAR > 0
ORDER BY SUM_TIMER_WAIT DESC;
