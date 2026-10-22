# Laravel production deploy — {name} ({environment}) → {domain}
#
# Smart pipeline: pulls {branch}, then only reinstalls dependencies whose
# manifests actually changed since the last deploy (tracked via the
# .laraenv-deployed-sha marker file). First deploy = everything runs.
# Set LARAENV_FORCE=1 to bypass the "already up to date" early exit.

OLD_SHA="$(cat .laraenv-deployed-sha 2>/dev/null || true)"
git fetch --all --prune
git reset --hard "origin/{branch}"
NEW_SHA="$(git rev-parse HEAD)"

if [ -n "$OLD_SHA" ] && [ "$OLD_SHA" = "$NEW_SHA" ] && [ "${LARAENV_FORCE:-0}" != "1" ]; then
  echo "Already at $NEW_SHA — nothing to deploy."
  exit 0
fi

# changed <paths...> — true on first deploy, when OLD_SHA is unreadable
# (force-push, shallow history), or when any of the paths differ.
changed() {
  [ -z "$OLD_SHA" ] && return 0
  ! git diff --quiet "$OLD_SHA" "$NEW_SHA" -- "$@" 2>/dev/null
}

# Maintenance mode (skip on first deploy — app isn't bootstrapped yet).
if [ -d vendor ]; then
  php artisan down --retry=30 || true
fi

if changed composer.json composer.lock; then
  composer install --no-dev --prefer-dist --optimize-autoloader --no-interaction
else
  echo "(composer deps unchanged — skipping composer install)"
fi

if changed package.json package-lock.json; then
  npm ci --no-audit --no-fund
  npm run build
elif changed resources/ vite.config.js vite.config.ts; then
  npm run build
else
  echo "(frontend unchanged — skipping npm build)"
fi

if [ -z "$OLD_SHA" ]; then
  php artisan key:generate --force
fi

php artisan migrate --force
php artisan optimize
php artisan queue:restart || true
php artisan up || true

printf '%s' "$NEW_SHA" > .laraenv-deployed-sha
echo "Deployed $NEW_SHA"
