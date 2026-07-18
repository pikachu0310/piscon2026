SELECT JSON_OBJECT(
  'schema_name', COALESCE(SCHEMA_NAME, ''),
  'digest', COALESCE(DIGEST, ''),
  'query', COALESCE(DIGEST_TEXT, ''),
  'calls', COUNT_STAR,
  'total_seconds', ROUND(SUM_TIMER_WAIT / 1000000000000, 6),
  'avg_ms', ROUND(AVG_TIMER_WAIT / 1000000000, 6),
  'max_ms', ROUND(MAX_TIMER_WAIT / 1000000000, 6),
  'lock_seconds', ROUND(SUM_LOCK_TIME / 1000000000000, 6),
  'errors', SUM_ERRORS,
  'warnings', SUM_WARNINGS,
  'rows_affected', SUM_ROWS_AFFECTED,
  'rows_sent', SUM_ROWS_SENT,
  'rows_examined', SUM_ROWS_EXAMINED,
  'rows_examined_per_call', ROUND(SUM_ROWS_EXAMINED / NULLIF(COUNT_STAR, 0), 2),
  'no_index_used', SUM_NO_INDEX_USED,
  'no_good_index_used', SUM_NO_GOOD_INDEX_USED,
  'full_scans', SUM_SELECT_SCAN,
  'tmp_tables', SUM_CREATED_TMP_TABLES,
  'tmp_disk_tables', SUM_CREATED_TMP_DISK_TABLES,
  'sort_rows', SUM_SORT_ROWS,
  'first_seen', FIRST_SEEN,
  'last_seen', LAST_SEEN
)
FROM performance_schema.events_statements_summary_by_digest
WHERE DIGEST IS NOT NULL
  AND COALESCE(SCHEMA_NAME, '') NOT IN (
    'mysql', 'sys', 'performance_schema', 'information_schema'
  )
ORDER BY SUM_TIMER_WAIT DESC;
