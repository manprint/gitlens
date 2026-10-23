-- No superuser; filesystem access revoked; extension creation restricted.
REVOKE EXECUTE ON FUNCTION pg_read_file(text)  FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION pg_ls_dir(text)     FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION pg_ls_waldir()      FROM PUBLIC;
-- pg_monitor holds EXECUTE on pg_ls_waldir() in its own right, so the PUBLIC
-- revoke above does not reach the monitoring roles. Revoke it from pg_monitor
-- too, so the profile takes the directory listing away from every tier.
REVOKE EXECUTE ON FUNCTION pg_ls_waldir()      FROM pg_monitor;
REVOKE CREATE ON DATABASE app FROM PUBLIC;
