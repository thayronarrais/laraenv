# SSH trust and saved passwords

These changes are included in LaraEnv 0.5.7; they are not present in the older 0.5.6 installer.

## Server identity

The integrated terminal, SFTP, deployment connections and proxy jumps use the same LaraEnv `ssh/known_hosts` as the external SSH client. A first-seen key is saved; a matching key is accepted. Changed/revoked keys and invalid trust files stop the connection. Verify the server fingerprint independently before intentionally updating a saved key. LaraEnv does not delete or replace a conflicting key automatically.

Servers offering several host keys negotiate a type already trusted for that hostname/port when possible. Trust verification still runs against the actual negotiated key before authentication. Concurrent first connections serialize updates; a conflicting concurrent key cannot replace the first persisted key.

## Passwords on disk

Saved SSH passwords are protected with Windows DPAPI for the current Windows account. The configuration uses a versioned protected envelope instead of plaintext password bytes. Legacy passwords are migrated with an atomic save; a protection or persistence failure preserves the original file and reports the error. Existing restrictive Windows file permissions are preserved during replacement.

This protection covers saved **SSH passwords**. It does not encrypt all configuration fields, cloud session tokens, sync keys or private-key files. Keep the environment directory and Windows account appropriately protected. Passwords are still available in application memory when needed for authentication and end-to-end encrypted sync.

## Moving a portable environment

DPAPI protection is bound to the Windows account/profile. After copying the environment to another account or machine, a saved password may be unavailable. The host and original protected bytes are preserved, while the UI asks for the password again. Enter and save a replacement on the destination account, use an available SSH key, or restore the password through encrypted sync with the correct sync passphrase.

An unavailable password is never pushed as an empty replacement to the cloud. Unrelated local changes preserve its protected bytes. A replacement is considered recovered only after successful protection and persistence. Copying a portable folder also does not transfer Windows trust in its local HTTPS authority.

## Verification

Host-key regressions use disposable in-memory SSH protocol fixtures rather than real servers. Password tests exercise DPAPI round trips, tampering, legacy migration, failed replacements and unavailable-account recovery in temporary directories. No user host, password or server is used by these tests.
