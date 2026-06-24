# i18n leaf-file migration contract

You are migrating hard-coded Chinese strings in ONE assigned set of handler files
to catalog-backed lookups. The i18n foundation already exists and is committed.

## How to localize (the only API you need)

Inside `package handler`, terse wrappers already exist (see `i18n_inject.go`):

- `tr(ctx, "key")` — plain text (most cases). Returns the localized string.
- `tr(ctx, "key", map[string]any{"Name": x})` — with template values.
- `trMd(ctx, "key", ...)` — localized AND MarkdownV2-escaped. Use ONLY when the
  surrounding message is sent with `ParseMode: telego.ModeMarkdownV2` AND the
  string is a plain label (no intentional markdown). If the code already builds
  markdown structure by hand and escapes dynamic parts, prefer `tr` and keep the
  structural markdown in code (see how about.go/texts.go do it).
- `trn(ctx, "key", count, ...)` — pluralized.

`ctx` is the `context.Context` already passed into every `Handle(ctx, b, update)`
and threaded through helpers. The router injects the request localizer into ctx,
so `tr` automatically renders the user's language. NEVER call i18n.For/Init.

## Where ctx comes from in callbacks/goroutines

- Every handler entrypoint has `ctx`. Pass it down.
- If a struct/closure outlives the request (async upload/refresh), capture the
  localizer at creation: `loc := i18n.From(ctx)` and later
  `tr(i18n.WithLocalizer(context.Background(), loc), "key")`. Look at how
  music.go's uploadTask.loc / queuedStatus.loc work and mirror that pattern.
  If a function has no ctx and is purely internal, add a `ctx context.Context`
  parameter and update its callers (they have ctx).

## Catalog keys — STRICT rules

1. Create THREE new shard files for your group only:
   `bot/i18n/locales/<DOMAIN>.en.toml`, `.zh.toml`, `.ja.toml`
   where <DOMAIN> is given in your task (e.g. `settings`). go-i18n merges all
   `locales/*.toml` into one bundle automatically — you do NOT edit en/zh/ja.toml.
2. Key names: snake_case, prefixed with your domain, e.g. `settings_title`,
   `settings_quality_lossless`. Keys must be UNIQUE across the whole catalog —
   your prefix guarantees that.
3. Author values as PLAIN text. NEVER hand-escape MarkdownV2 (no `\.`, `\-`).
   Escaping happens at the output boundary via trMd. Keep `{{.Var}}` placeholders
   for dynamic values; pass them via the map arg.
4. zh value = the EXACT original Chinese string you are replacing (preserve emoji,
   spacing, punctuation). en = faithful English. ja = faithful Japanese.
5. ALL THREE shards must contain the SAME set of keys (full parity).

## Hard constraints

- Do NOT edit: en.toml, zh.toml, ja.toml, texts_compat.go, i18n_inject.go,
  any file outside your assigned set, or any *_test.go you weren't given.
- Do NOT change exported helper signatures shared with other files unless told.
  If you think a shared helper (in helpers.go/texts.go) needs ctx, DOCUMENT it in
  your final report instead of editing it — the coordinator will handle it.
- Keep behavior identical. Only the string source changes.
- Some strings are NOT user-facing (log messages via h.Logger.*, error wrapping
  with fmt.Errorf, callback DATA tokens like "settings platform", protocol
  literals). DO NOT localize those — only text shown to the user in SendMessage /
  EditMessageText / AnswerCallbackQuery Text / caption / button Text.
- A few display strings double as sentinels (strings.Contains checks). If you see
  a string compared against message text, flag it; don't naively localize both
  sides. (The big ones are already handled.)

## Before you finish

1. `gofmt -w` your changed files.
2. `go build ./...` MUST pass from your worktree root.
3. `go test ./bot/telegram/handler/ ./bot/i18n/` MUST pass.
4. Verify shard key parity: all three shards have identical keys.
5. Report: files changed, shard filenames, # strings migrated, and any shared-
   helper changes you had to defer.
