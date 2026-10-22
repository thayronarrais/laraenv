import { useI18n } from "../lib/i18n";

// The selector reads its options from the catalogues themselves, so adding a
// language is dropping a file into langs/ — nothing here lists languages.
// That includes a locale that exists only on disk (paths.Root()/langs): it
// arrives through the same `locales` list, built from the merged catalogues.
export function LanguageSelector() {
  const { lang, locales, setLang, t, overrideDir, overrideResults } = useI18n();
  // Only the active locale's own contribution is shown — this is about
  // knowing which screen you're looking at, not a global inventory of every
  // override on disk.
  const activeOverride = overrideResults.find((r) => r.locale === lang);
  const activeLocale = locales.find((l) => l.code === lang);
  return (
    <div className="flex flex-col gap-2">
      <label className="text-sm text-white/70" htmlFor="language">
        {t("settings.appearance.language")}
      </label>
      <select
        id="language"
        value={lang}
        onChange={(e) => void setLang(e.target.value)}
        className="rounded-lg bg-white/5 px-3 py-2 text-sm text-white outline-none ring-1 ring-white/10 focus:ring-white/30"
      >
        {locales.map((l) => (
          <option key={l.code} value={l.code}>
            {l.nativeName}
            {l.needsNativeReview ? ` ${t("settings.appearance.needsNativeReviewBadge")}` : ""}
          </option>
        ))}
      </select>
      <p className="text-xs text-white/50">{t("settings.appearance.languageHint")}</p>
      {activeLocale?.needsNativeReview && (
        <p className="text-xs text-amber-300/80">{t("settings.appearance.needsNativeReviewNotice")}</p>
      )}
      {overrideDir && activeOverride && (
        <div className="text-xs space-y-1">
          <p className="text-white/50">
            {t("settings.appearance.overrideActive", { path: overrideDir })}
          </p>
          {(activeOverride.rejected?.length ?? 0) > 0 && (
            <p className="text-amber-300/80">
              {t("settings.appearance.overrideRejected", {
                count: activeOverride.rejected.length,
                keys: activeOverride.rejected.map((r) => r.key).join(", "),
              })}
            </p>
          )}
          {activeOverride.unreadable && (
            <p className="text-amber-300/80">
              {t("settings.appearance.overrideUnreadable", {
                file: `${activeOverride.locale}.json`,
                reason: activeOverride.unreadable,
              })}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
