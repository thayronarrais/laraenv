IDENTITY_FILE="$DIR/$PROJECT.owned"
if [ ! -f "$IDENTITY_FILE" ]; then IDENTITY_FILE="$DIR/$PROJECT.bootstrap"; fi
if [ ! -f "$IDENTITY_FILE" ]; then exit 0; fi
read_database_identity "$IDENTITY_FILE"

HAS_ROLE=$(runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 -Atc "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='$DB_USER')")
if [ "$IDENTITY_FILE" = "$DIR/$PROJECT.bootstrap" ] && [ "$HAS_ROLE" = t ]; then
    DB_PASS=$(read_env DB_PASSWORD "$IDENTITY_FILE")
    if ! PGPASSWORD="$DB_PASS" psql -X -h 127.0.0.1 -p 5432 -U "$DB_USER" -d postgres -Atc 'SELECT 1' >/dev/null; then
        echo 'ERROR: refusing to drop a PostgreSQL role that does not match pending bootstrap credentials.' >&2
        exit 1
    fi
fi
HAS_DB=$(runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 -Atc "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname='$DB_NAME')")
if [ "$HAS_DB" = t ]; then
    OWNER=$(runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 -Atc "SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname='$DB_NAME'")
    if [ "$OWNER" != "$DB_USER" ]; then echo 'ERROR: refusing to drop a PostgreSQL database owned by another role.' >&2; exit 1; fi
    # Prevent reconnects before ending sessions, scoped to the verified owned DB.
    runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 <<SQL
ALTER DATABASE "$DB_NAME" WITH ALLOW_CONNECTIONS false;
SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='$DB_NAME' AND pid<>pg_backend_pid();
SQL
    for attempt in $(seq 1 50); do
        CONNECTED=$(runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 -Atc "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname='$DB_NAME')")
        [ "$CONNECTED" != t ] && break
        sleep 0.1
    done
fi
runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 <<SQL
DROP DATABASE IF EXISTS "$DB_NAME";
DROP ROLE IF EXISTS "$DB_USER";
SQL
rm -f "$DIR/$PROJECT.owned" "$DIR/$PROJECT.bootstrap"
