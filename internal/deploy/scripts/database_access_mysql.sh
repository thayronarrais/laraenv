PORT=3306
DROP_FILE="$DIR/$PROJECT.drop.sql"

# Saved revocations contain no passwords and remove only accounts we created.
if [ -f "$DROP_FILE" ]; then mysql < "$DROP_FILE"; rm -f "$DROP_FILE"; fi
remove_firewall_rules
rm -f "$DIR/$PROJECT.remote"

if [ "$REMOTE" = 1 ]; then
    read_database_identity
    CREATE_USER=$(mysql --batch --raw --skip-column-names -e "SHOW CREATE USER '$DB_USER'@'localhost'" | cut -f2-)
    if [[ "$CREATE_USER" != "CREATE USER '$DB_USER'@'localhost'"* ]]; then
        echo 'ERROR: cannot read the dedicated localhost MySQL account.' >&2
        exit 1
    fi
    GRANT_DB="$DB_NAME"
    PARTIAL_REVOKES=$(mysql --batch --skip-column-names -e 'SELECT @@partial_revokes' 2>/dev/null || true)
    if [ "$PARTIAL_REVOKES" != 1 ]; then GRANT_DB=$(printf '%s' "$DB_NAME" | sed 's/_/\\_/g'); fi
    for ip in $ALLOWED_IPS; do
        # Do not overwrite a pre-existing account managed outside this deploy.
        EXISTS=$(mysql --batch --skip-column-names -e "SELECT COUNT(*) FROM mysql.user WHERE User='$DB_USER' AND Host='$ip'")
        if [ "$EXISTS" != 0 ]; then
            echo "ERROR: a MySQL account for $DB_USER at $ip already exists outside this deployment." >&2
            exit 1
        fi
        printf "DROP USER IF EXISTS '%s'@'%s';\n" "$DB_USER" "$ip" >> "$DROP_FILE"
        REMOTE_CREATE="${CREATE_USER/\'@\'localhost\'/\'@\'$ip\'}"
        printf '%s;\nGRANT ALL ON `%s`.* TO '\''%s'\''@'\''%s'\'';\n' "$REMOTE_CREATE" "$GRANT_DB" "$DB_USER" "$ip" | mysql
    done
    add_firewall_rules
    touch "$DIR/$PROJECT.remote"
fi

# Listener is shared by all deployments using this engine.
LISTEN=127.0.0.1
if compgen -G "$DIR/*.remote" >/dev/null; then LISTEN='*'; fi
CONF=/etc/mysql/conf.d/zz-laraenv-access.cnf
if [ -d /etc/mysql/mysql.conf.d ]; then CONF=/etc/mysql/mysql.conf.d/zz-laraenv-access.cnf;
elif [ -d /etc/mysql/mariadb.conf.d ]; then CONF=/etc/mysql/mariadb.conf.d/zz-laraenv-access.cnf; fi
TMP=$(mktemp)
printf '[mysqld]\nbind-address = %s\nloose-mysqlx-bind-address = 127.0.0.1\n' "$LISTEN" > "$TMP"
if ! cmp -s "$TMP" "$CONF"; then
    install -m 644 "$TMP" "$CONF"
fi
rm -f "$TMP"
# Always restart so reapplying also recovers a previously failed restart.
systemctl restart mysql
echo 'MySQL database access configured.'
