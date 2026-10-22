# Laravel development deploy — {name} ({environment}) → {domain}
#
# Lighter flow than production: dev dependencies stay in, caches are cleared
# instead of rebuilt, no maintenance mode or queue restart. Dependency steps
# only run when the matching manifest changed since the last deploy.

OLD_SHA="$(cat .laraenv-deployed-sha 2>/dev/null || true)"
git fetch --all --prune
git reset --hard "origin/{branch}"
NEW_SHA="$(git rev-parse HEAD)"

if [ -n "$OLD_SHA" ] && [ "$OLD_SHA" = "$NEW_SHA" ] && [ "${LARAENV_FORCE:-0}" != "1" ]; then
  echo "Already at $NEW_SHA — nothing to deploy."
  exit 0
fi

changed() {
  [ -z "$OLD_SHA" ] && return 0
  ! git diff --quiet "$OLD_SHA" "$NEW_SHA" -- "$@" 2>/dev/null
}

if changed composer.json composer.lock; then
  composer install --prefer-dist --no-interaction
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

php artisan migrate
php artisan optimize:clear

printf '%s' "$NEW_SHA" > .laraenv-deployed-sha
echo "Deployed $NEW_SHA"
