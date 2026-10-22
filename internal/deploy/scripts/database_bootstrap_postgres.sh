cd "$DEPLOY_ROOT"
PENDING="$DIR/$PROJECT.bootstrap"
OWNED="$DIR/$PROJECT.owned"
if [ ! -f "$PENDING" ] && [ "$(read_env DB_CONNECTION)" = pgsql ]; then
    read_database_identity
    DB_PASS=$(read_env DB_PASSWORD)
    if [ -n "$DB_PASS" ] && PGPASSWORD="$DB_PASS" psql -X -h 127.0.0.1 -p 5432 -U "$DB_USER" -d "$DB_NAME" -Atc 'SELECT 1' >/dev/null; then
        echo '(existing PostgreSQL credentials authenticate — preserving .env)'
        exit 0
    fi
    echo 'ERROR: existing PostgreSQL credentials do not authenticate. Correct .env before reapplying; no password was changed.' >&2
    exit 1
fi

if [ -f "$PENDING" ]; then
    read_database_identity "$PENDING"
    DB_PASS=$(read_env DB_PASSWORD "$PENDING")
    if [[ ! "$DB_PASS" =~ ^[a-f0-9]{48}$ ]]; then echo 'ERROR: invalid pending PostgreSQL bootstrap state.' >&2; exit 1; fi
else
    DB_SAFE=$(printf '%s' "$PROJECT" | tr '.-' '__')
    [[ "$DB_SAFE" =~ ^[a-zA-Z_] ]] || DB_SAFE="project_$DB_SAFE"
    case "$DB_SAFE" in root|postgres|mysql) DB_SAFE="project_$DB_SAFE";; esac
    DB_NAME="${DB_SAFE:0:60}_db"
    DB_USER="${DB_SAFE:0:32}"
    EXISTS=$(runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 -Atc "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='$DB_USER') OR EXISTS (SELECT 1 FROM pg_database WHERE datname='$DB_NAME')")
    if [ "$EXISTS" != f ]; then
        echo 'ERROR: PostgreSQL project role or database already exists. Set its credentials in .env before reapplying.' >&2
        exit 1
    fi
    DB_PASS=$(openssl rand -hex 24)
    TMP=$(mktemp "$DIR/.bootstrap-XXXXXX")
    printf 'DB_DATABASE=%s\nDB_USERNAME=%s\nDB_PASSWORD=%s\n' "$DB_NAME" "$DB_USER" "$DB_PASS" > "$TMP"
    mv "$TMP" "$PENDING"
fi

HAS_ROLE=$(runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 -Atc "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='$DB_USER')")
if [ "$HAS_ROLE" = f ]; then
    runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 <<SQL
SET password_encryption = 'scram-sha-256';
CREATE ROLE "$DB_USER" LOGIN PASSWORD '$DB_PASS';
SQL
elif ! PGPASSWORD="$DB_PASS" psql -X -h 127.0.0.1 -p 5432 -U "$DB_USER" -d postgres -Atc 'SELECT 1' >/dev/null; then
    echo 'ERROR: pending PostgreSQL role does not match saved bootstrap credentials.' >&2
    exit 1
fi

HAS_DB=$(runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 -Atc "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname='$DB_NAME')")
if [ "$HAS_DB" = f ]; then
    runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 <<SQL
CREATE DATABASE "$DB_NAME" OWNER "$DB_USER";
SQL
else
    OWNER=$(runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 -Atc "SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname='$DB_NAME'")
    if [ "$OWNER" != "$DB_USER" ]; then echo 'ERROR: pending PostgreSQL database belongs to another role.' >&2; exit 1; fi
fi

TMP=$(mktemp "$DIR/.owned-XXXXXX")
printf 'DB_DATABASE=%s\nDB_USERNAME=%s\n' "$DB_NAME" "$DB_USER" > "$TMP"
mv "$TMP" "$OWNED"
ENV_TMP=$(mktemp "$DEPLOY_ROOT/.env.laraenv-XXXXXX")
cp -p .env "$ENV_TMP"
trap 'rm -f "$ENV_TMP"' EXIT

set_env_var() {
    local key="$1" value="$2"
    if grep -qE "^${key}=" "$ENV_TMP"; then
        sed -i "s|^${key}=.*|${key}=${value}|" "$ENV_TMP"
    elif grep -qE "^[[:space:]]*#[[:space:]]*${key}=" "$ENV_TMP"; then
        sed -i "s|^[[:space:]]*#[[:space:]]*${key}=.*|${key}=${value}|" "$ENV_TMP"
    else
        printf '%s=%s\n' "$key" "$value" >> "$ENV_TMP"
    fi
}
set_env_var DB_CONNECTION pgsql
set_env_var DB_HOST 127.0.0.1
set_env_var DB_PORT 5432
set_env_var DB_DATABASE "$DB_NAME"
set_env_var DB_USERNAME "$DB_USER"
set_env_var DB_PASSWORD "$DB_PASS"
mv "$ENV_TMP" .env
rm -f "$PENDING"
echo "PostgreSQL DB ready: $DB_NAME / user $DB_USER"
