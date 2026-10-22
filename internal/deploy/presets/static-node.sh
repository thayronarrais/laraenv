# Static / Node deploy — {name} ({environment}) → {domain}
#
# Pulls {branch} and rebuilds the frontend only when package files changed.
# Repos without package.json are treated as plain static files.

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

if [ -f package.json ]; then
  if changed package.json package-lock.json; then
    npm ci --no-audit --no-fund
  else
    echo "(node deps unchanged — skipping npm ci)"
  fi
  if grep -q '"build"' package.json; then
    npm run build
  else
    echo "(no build script in package.json — skipping build)"
  fi
else
  echo "(no package.json — static files only, nothing to build)"
fi

printf '%s' "$NEW_SHA" > .laraenv-deployed-sha
echo "Deployed $NEW_SHA"
