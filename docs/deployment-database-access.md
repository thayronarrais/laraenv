# Remote database access during deployment

The Apply form includes **Allow remote database access** for MySQL and PostgreSQL.
It is off by default, including when reopening deployments created before this
option existed. Changing it takes effect when Apply runs again, rather than on
each automatic Git pull.

When enabled, enter individual IPv4 or IPv6 addresses separated by commas or
lines. The backend validates and deduplicates them. Hostnames, wildcards, CIDR
ranges, unspecified addresses and multicast addresses are rejected.

Remote access requires an installed and active UFW firewall. LaraEnv adds only
source-specific TCP rules and does not enable the host firewall, since doing so
could interrupt SSH. Allow SSH before enabling UFW. A provider's separate network
firewall may also need a rule for those source IPs on port 3306 or 5432.

Laravel/MySQL uses the existing project database bootstrap. For Laravel with
PostgreSQL and remote access enabled, Apply creates a dedicated project role and
database when the project has no PostgreSQL credentials yet, then updates its
DB_* entries in .env. Working PostgreSQL credentials are preserved; conflicting
existing roles/databases or invalid saved credentials stop Apply rather than
resetting their passwords. Other project types need a configured project .env
with a dedicated database user and working credentials.

If provisioning is interrupted, generated credentials stay in root-only host
state and are reused on the next Apply. PostgreSQL resource ownership is tracked
separately so Destroy removes only roles/databases created by this deployment.

MySQL gets an account for each permitted address, using the localhost account's
authentication settings and grants limited to the project database. PostgreSQL
gets password-based SCRAM rules before existing host rules, followed by explicit
remote rejection for the project role. The application continues connecting
locally. Passwords stay on the server.
PostgreSQL project roles must have no administrator flags or role memberships.

Reapplying with remote access off revokes the accounts/rules created for this
deployment. Removing or changing the selected database engine also cleans up
managed access on the previous engine. Destroy revokes managed access before
dropping the database. Unrelated UFW rules are preserved.
Revocation also ends now-disallowed remote PostgreSQL sessions, preserving
local connections and allowed sessions of other projects when only reloading.

The database listener is shared by projects using the same engine. It stays
remote-capable while another LaraEnv deployment requires it; restrictions for
each project's account remain separate. With no managed remote deployments,
the listener is set back to localhost. Apply may restart the database service
to change the listener. Use a distinct project database user per deployment.
Existing remote accounts and permissions configured outside LaraEnv are not
deleted by this option.

The encrypted deployment's apply configuration stores:

```json
{
  "database": "mysql",
  "database_remote_access": true,
  "database_allowed_ips": ["203.0.113.42", "2001:db8::2"]
}
```
