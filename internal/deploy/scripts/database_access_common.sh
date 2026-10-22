set -euo pipefail
umask 077

# UFW must already be active. Enabling a host firewall here could lock out SSH.
if [ "$REMOTE" = 1 ]; then
    if ! command -v ufw >/dev/null || ! LC_ALL=C ufw status | grep -q '^Status: active$'; then
        echo 'ERROR: remote database access requires an installed, active UFW firewall. Configure it with SSH allowed, then reapply.' >&2
        exit 1
    fi
fi

STATE=/etc/laraenv/database-access
mkdir -p "$STATE/$ENGINE"
chmod 700 "$STATE" "$STATE/$ENGINE"
exec 9>"$STATE/access.lock"
flock -x 9
DIR="$STATE/$ENGINE"
TAG="laraenv-db-$ENGINE-$PROJECT"

read_env() {
    local value
    value=$(sed -n "s/^$1=//p" "${2:-$DEPLOY_ROOT/.env}" | tail -n 1 | tr -d '\r')
    if [[ "$value" == \"*\" ]] || [[ "$value" == \'*\' ]]; then value="${value:1:${#value}-2}"; fi
    printf '%s' "$value"
}

read_database_identity() {
    local identity_file="${1:-$DEPLOY_ROOT/.env}"
    if [ ! -f "$identity_file" ]; then
        echo 'ERROR: remote database access needs DB_DATABASE and DB_USERNAME in the project .env.' >&2
        exit 1
    fi
    DB_NAME=$(read_env DB_DATABASE "$identity_file")
    DB_USER=$(read_env DB_USERNAME "$identity_file")
    if [[ ! "$DB_NAME" =~ ^[a-zA-Z_][a-zA-Z0-9_]{0,62}$ ]] || [[ ! "$DB_USER" =~ ^[a-zA-Z_][a-zA-Z0-9_]{0,31}$ ]]; then
        echo 'ERROR: DB_DATABASE and DB_USERNAME must be plain SQL identifiers (up to 63 and 32 characters).' >&2
        exit 1
    fi
    case "$DB_USER" in root|postgres|mysql) echo 'ERROR: use a dedicated project database user for remote access.' >&2; exit 1;; esac
    case "$DB_NAME" in mysql|information_schema|performance_schema|sys|postgres|template0|template1)
        echo 'ERROR: remote access must target a project database, not a system database.' >&2; exit 1;; esac
}

remove_firewall_rules() {
    if command -v ufw >/dev/null; then
        local rules number
        rules=$(LC_ALL=C ufw status numbered)
        while read -r number; do
            [ -z "$number" ] || ufw --force delete "$number"
        done < <(printf '%s\n' "$rules" | awk -v tag="$TAG" '
            /\[/ { comment=$0; sub(/^.*# */, "", comment); sub(/ *$/, "", comment);
                if (comment==tag) { number=$0; sub(/^\[ */, "", number); sub(/\].*$/, "", number); print number } }
        ' | sort -rn)
    fi
}

add_firewall_rules() {
    local ip
    for ip in $ALLOWED_IPS; do
        ufw prepend allow from "$ip" to any port "$PORT" proto tcp comment "$TAG"
    done
}
