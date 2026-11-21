/** The PostgreSQL settings auto_explain plans need (plan 007 R4); names stay English. */
export const AUTO_PLAN_SETTINGS = [
  "shared_preload_libraries = 'auto_explain'",
  'auto_explain.log_min_duration = 250',
  'auto_explain.log_format = json',
  'compute_query_id = on',
  'logging_collector = on',
  "log_destination = 'jsonlog'",
] as const
