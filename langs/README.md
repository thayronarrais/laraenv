# Translations

The LaraEnv interface reads its text from the JSON files in this directory.
They live here in the open. Anyone can add a language: fork, edit, open a
pull request. The build downloads these files and puts them in the app.

## Adding a language

1. Copy `en.json` to `<code>.json`. Use a BCP-47 tag for `<code>`, like `ru`,
   `de`, `fr`, or `es-MX`.
2. Change the `$meta` block at the top. `nativeName` is shown in the language
   selector. Write it the way your language writes it:

   ```json
   "$meta": { "code": "ru", "name": "Russian", "nativeName": "Русский" }
   ```

3. Translate the values. Do not change any key. Keys are code, not text.
4. Open a pull request. CI checks your file and tells you what is missing.

## Machine-translated languages

Some languages ship without a native speaker checking them first. That is
better than not shipping the language at all, but the app has to say so.

If a language has not been checked by a native speaker, add this to its
`$meta` block:

```json
"$meta": {
  "code": "ru",
  "name": "Russian",
  "nativeName": "Русский",
  "needsNativeReview": true
}
```

This does two things. The language selector marks the language before
someone picks it, and it shows a short note, in that language, explaining
that the translation is machine-made and inviting a fix.

Leave the flag off, or set it to `false`, once a native speaker has read the
file. If you review a language and fix what needs fixing, remove
`needsNativeReview` in the same pull request. Do not open a second PR just
for that.

## The rules CI enforces

**Keep `{{placeholders}}` in English.** `{{name}}` is a slot. The app fills
it in with real text. If you translate it to `{{nome}}`, the app cannot find
it, and `{{nome}}` shows up literally on screen.

```json
"created": "Проект {{name}} создан"
```

**Write plurals as an object, not as a sentence with an "(s)".** Different
languages need different plural forms. CI knows which forms your language
needs:

```json
"count": {
  "one":   "{{count}} проект",
  "few":   "{{count}} проекта",
  "many":  "{{count}} проектов",
  "other": "{{count}} проекта"
}
```

English needs `one` and `other`. Russian needs `one`, `few`, `many`, and
`other`. Japanese needs only `other`. Every language needs `other`. If you
leave one out, CI tells you which one.

**If you look at `pt-BR.json` as an example, do not copy its plural forms
as if `many` and `other` say the same thing.** Brazilian Portuguese needs
three plural forms: `one`, `many`, and `other`. English only has two, so it
is easy to assume `many` is just a copy of `other`. It is not. Portuguese
adds the word "de" before the noun at round millions:

```
1.000.000 de pulls   ← many
1.000.001 pulls       ← other
```

So `many` and `other` are genuinely different text in Portuguese. Pasting
`other` into `many` will pass the CI check, because CI only checks that the
form exists, not what it says. But it is still wrong. When you add a
language, check what each plural form actually needs to say. Do not assume
two forms are the same just because English does not separate them.

**Do not add or remove keys.** Your file must have the exact same keys as
`en.json`. An extra key is almost always a typo in a path. CI treats it as
an error.

**You can skip the `errors.*` section for now.** These are technical
messages. If you leave them untranslated, CI gives a warning, not an error.
Your pull request can still be merged, and you can add them later.

**A few values contain an English fragment you must not touch.** A handful
of strings are read by the app's own code, not just shown to the user — for
example, an error message that the frontend checks for a fixed English
phrase, to decide whether to offer a "fallback" option. Translate the
sentence normally, but keep that exact fragment, character for character,
somewhere in your translation. `langs/.do-not-translate.json` lists every
such key and the exact text each one needs; CI checks it and, if your
translation is missing the required fragment, tells you the key and the
missing text — this used to fail silently (no error, no crash, just a
missing feature), so if you hit this error, it is not a bug in the checker.

For example, `errors.ssh.fileTooLarge` in English is:

```
file too large: {{size}} bytes (limit {{limit}})
```

A Brazilian Portuguese translation keeps `file too large` verbatim and
translates the rest:

```
file too large: o arquivo tem {{size}} bytes, mas o limite é {{limit}}
```

Yes, it reads oddly — an English fragment sitting inside a Portuguese
sentence. That is the honest cost of the app matching on it: an odd sentence
that works beats a clean one that silently breaks the feature. If a required
fragment does not fit anywhere in your language's natural phrasing, open an
issue and ask; do not just drop it to make the checker pass some other way.

**Some sentences are cut into pieces.** Look for keys that end in `Prefix`
and `Suffix`, or `Part1` and `Part2`. These are one sentence, split into
pieces, so the app can put an icon, a button, or bold text in the middle.
The translation function only returns plain text, so it cannot include that
markup itself. That's why the sentence has to be cut somewhere.

English cuts the sentence at a certain point. Your language might read
better with the cut in a different place.

- You may move words from one piece to another to get natural word order.
- The cut points are not fixed. They are just where English happened to
  split the sentence.
- Check that each piece still reads well next to whatever sits between it
  and the next piece (an icon, a value, a `<span>`) in the actual app.

## Do not edit `en.json`

English text comes from the application source code. It is copied here at
every release. If you change `en.json`, your change will be overwritten and
lost. If some English text is wrong or unclear, open an issue instead. That
fixes it at the source.

## Checking your work locally

You need Node 18 or newer. No install step, no dependencies:

```bash
node tools/langs/check.mjs langs
```
