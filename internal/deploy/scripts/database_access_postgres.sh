PORT=5432
HBA=$(runuser -u postgres -- psql -X -Atc 'SHOW hba_file')
CONF=$(runuser -u postgres -- psql -X -Atc 'SHOW config_file')
if [ ! -f "$HBA" ] || [ ! -f "$CONF" ]; then
    echo 'ERROR: cannot locate PostgreSQL configuration for the default cluster.' >&2
    exit 1
fi

RULE_FILE="$DIR/$PROJECT.hba"
if [ "$REMOTE" = 1 ]; then
    read_database_identity
    PRIVILEGED=$(runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 -Atc "SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls OR EXISTS (SELECT 1 FROM pg_auth_members WHERE member=pg_roles.oid) FROM pg_roles WHERE rolname='$DB_USER'")
    if [ "$PRIVILEGED" != f ]; then
        echo 'ERROR: remote database access requires a non-administrator PostgreSQL project role.' >&2
        exit 1
    fi
    for rules in "$DIR"/*.hba; do
        if [ "$rules" != "$RULE_FILE" ] && [ -f "$rules" ] && awk -v user="$DB_USER" '{ role=$3; gsub(/"/, "", role); if (role==user) found=1 } END { exit !found }' "$rules"; then
            echo 'ERROR: this PostgreSQL role is already managed by another deployment. Use a dedicated role per project.' >&2
            exit 1
        fi
    done
    DB_PASS=$(read_env DB_PASSWORD)
    if [ -z "$DB_PASS" ] || ! PGPASSWORD="$DB_PASS" psql -X -h 127.0.0.1 -p 5432 -U "$DB_USER" -d "$DB_NAME" -Atc 'SELECT 1' >/dev/null; then
        echo 'ERROR: configure working PostgreSQL credentials in the project .env before allowing remote access.' >&2
        exit 1
    fi
    # Existing MD5 verifiers cannot authenticate against SCRAM HBA entries.
    # Keep the working password, but store a SCRAM verifier for this role.
    SQL_PASS=$(printf '%s' "$DB_PASS" | sed "s/'/''/g")
    runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 <<SQL
SET standard_conforming_strings = on;
SET password_encryption = 'scram-sha-256';
ALTER ROLE "$DB_USER" PASSWORD '$SQL_PASS';
SQL
    TMP=$(mktemp)
    printf 'host "%s" "%s" 127.0.0.1/32 scram-sha-256\nhost "%s" "%s" ::1/128 scram-sha-256\n' "$DB_NAME" "$DB_USER" "$DB_NAME" "$DB_USER" > "$TMP"
    for ip in $ALLOWED_IPS; do
        MASK=32; [[ "$ip" != *:* ]] || MASK=128
        printf 'host "%s" "%s" %s/%s scram-sha-256\n' "$DB_NAME" "$DB_USER" "$ip" "$MASK" >> "$TMP"
    done
    printf 'host all "%s" 0.0.0.0/0 reject\nhost all "%s" ::/0 reject\n' "$DB_USER" "$DB_USER" >> "$TMP"
    # Credential changes must not expose the old identity through an existing
    # permissive host rule. Keep only its local entries and remote rejections.
    if [ -f "$RULE_FILE" ]; then
        awk -v user="$DB_USER" '{ role=$3; gsub(/"/, "", role); if (role!=user && ($4=="127.0.0.1/32" || $4=="::1/128" || $5=="reject")) print }' "$RULE_FILE" >> "$TMP"
    fi
    mv "$TMP" "$RULE_FILE"
else
    # Retain the local/reject entries when disabling, even on a host with
    # permissive pre-existing HBA entries further down the file.
    if [ -f "$RULE_FILE" ]; then
        TMP=$(mktemp)
        awk '$4=="127.0.0.1/32" || $4=="::1/128" || $5=="reject"' "$RULE_FILE" > "$TMP"
        mv "$TMP" "$RULE_FILE"
    fi
fi

remove_firewall_rules
rm -f "$DIR/$PROJECT.remote"
if [ "$REMOTE" = 1 ]; then add_firewall_rules; touch "$DIR/$PROJECT.remote"; fi

TMP=$(mktemp)
printf '# BEGIN LARAENV DATABASE ACCESS\n' > "$TMP"
for rules in "$DIR"/*.hba; do [ ! -f "$rules" ] || cat "$rules" >> "$TMP"; done
printf '# END LARAENV DATABASE ACCESS\n' >> "$TMP"
awk '/^# BEGIN LARAENV DATABASE ACCESS$/ { skip=1; next }
     /^# END LARAENV DATABASE ACCESS$/ { skip=0; next }
     !skip { print }' "$HBA" >> "$TMP"
cat "$TMP" > "$HBA"
rm -f "$TMP"

LISTEN=localhost
if compgen -G "$DIR/*.remote" >/dev/null; then LISTEN='*'; fi
# ALTER SYSTEM takes precedence over both included settings and auto.conf.
CURRENT=$(runuser -u postgres -- psql -X -Atc 'SHOW listen_addresses')
runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 -c "ALTER SYSTEM SET listen_addresses = '$LISTEN'"
if [ "$CURRENT" != "$LISTEN" ]; then systemctl restart postgresql; else systemctl reload postgresql; fi

# Reloading HBA only affects new connections. End now-disallowed remote
# sessions of this project's roles, preserving local and unrelated sessions.
if [ -f "$RULE_FILE" ]; then
    USERS=''
    for role in $(awk '$1=="host" { role=$3; gsub(/"/, "", role); print role }' "$RULE_FILE" | sort -u); do
        [[ "$role" =~ ^[a-zA-Z_][a-zA-Z0-9_]{0,31}$ ]] || { echo 'ERROR: invalid managed PostgreSQL role.' >&2; exit 1; }
        USERS="${USERS:+$USERS,}'$role'"
    done
    KEEP=false
    if [ "$REMOTE" = 1 ]; then
        IPS=''
        for ip in $ALLOWED_IPS; do IPS="${IPS:+$IPS,}'$ip'::inet"; done
        KEEP="usename='$DB_USER' AND datname='$DB_NAME' AND client_addr IN ($IPS)"
    fi
    if [ -n "$USERS" ]; then
        runuser -u postgres -- psql -X -v ON_ERROR_STOP=1 -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid<>pg_backend_pid() AND usename IN ($USERS) AND client_addr IS NOT NULL AND NOT (client_addr <<= inet '127.0.0.0/8' OR client_addr=inet '::1') AND NOT ($KEEP)"
    fi
fi
echo 'PostgreSQL database access configured.'
