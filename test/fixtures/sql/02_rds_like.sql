-- No superuser; filesystem access revoked; extension creation restricted.
REVOKE EXECUTE ON FUNCTION pg_read_file(text)  FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION pg_ls_dir(text)     FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION pg_ls_waldir()      FROM PUBLIC;
REVOKE CREATE ON DATABASE app FROM PUBLIC;
