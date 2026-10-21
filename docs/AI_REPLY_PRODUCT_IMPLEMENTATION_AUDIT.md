# AI Reply — product implementation audit and contracts (20.10.2026)

Scope: onboarding redesign, keyboard and paste permissions, permission diagnostics and reminders,
recipient types with icons, 50 welcome replies, response top-ups through StoreKit 2 / Google Play
Billing, human support chat, admin sections, emoji page in both keyboards.

This file is the single source of truth for cross-platform contracts. Backend, iOS and Android
follow it; when an implementation has to deviate, the deviation is written here.

## 1. Audit summary

### What exists and is reused
- Backend (Go 1.24, SQLite, one writer, `BEGIN IMMEDIATE`): plan quota reserved atomically in
  `repository.ReserveQuota` and refunded on provider failure; `/api/v1/config` `features` flags;
  push through FCM HTTP v1 for both platforms with dedupe keys, leases, retries, campaigns and
  delivery history; `system_settings` with cached readers (`internal/limits` pattern); admin audit
  log (`admin.Service.Audit`); admin SPA in `internal/transport/web/static/admin-app.js` with
  kk/ru/en/uz dictionaries checked by `admin_i18n_test`.
- iOS: onboarding v2 (versioned, resumable, reopenable from Settings), keyboard setup screen,
  App Group heartbeat `shared.keyboardLastSeen` / `shared.keyboardFullAccess`, Darwin
  notifications `kz.ai-reply.keyboard.appeared.full|.limited`, persona row
  `[✨][chips]`, deep links `aireply://<screen>` from push.
- Android: onboarding v2, keyboard status from `InputMethodManager` + `DEFAULT_INPUT_METHOD`,
  persona row `[✨][chips][🎙]`, one-height page model with pads, FCM, deep links.

### What is missing
- Welcome bonus, credit ledger, idempotent generation, purchases of packs or passes, store
  verification (only `payments.DemoProvider` for plan checkout exists).
- Permission diagnostics storage/API, reminder types, cooldown ledger, admin diagnostics view.
- Support chat (no tables, no endpoints, no UI).
- Recipient icons; labels still read Бизнес/Работа; app reply screens ignore the keyboard's
  selected persona.
- Emoji page on both keyboards.

### Defects found (fixed as part of this work)
- iOS `KeyboardStatusMonitor` keeps «Full Access on» forever: the extension cannot write the App
  Group without Full Access, so a stale `true` is never corrected. Status gets timestamps and is
  shown as «verified at …», never as current truth.
- iOS keyboard: a denied paste (`hasStrings == true`, `string == nil`) is reported as «copy a
  message first»; the empty source card cannot be typed into; Retry is offered for errors a retry
  cannot fix.
- iOS app: Paste reads `UIPasteboard.general.string` (system prompt every time, silent failure);
  usage card is red «0» before the first `/me` answer.
- Android: app paste ignores `EXTRA_IS_SENSITIVE`; IME shows «Try again» in red for an empty
  clipboard; sensitive clips are reported as «copy a message first»; red «0 / 0» for non-daily
  allowances; `subscription_per_day` is not a plural.
- Backend: no age check for queued pushes (a quota notice can arrive after the quota reset); no
  quiet hours for sweep notices; `no_devices` also used for «devices exist but unreachable»;
  `PUSH_BATCH_SIZE` / `PUSH_WORKER_CONCURRENCY` unbounded against the 5 min lease; payment
  confirm can re-grant a failed/refunded payment; quota refusals counted as failed generations;
  admin user search does not escape `%`/`_`; admin user detail shows a lapsed subscription as
  active.

### Compliance notes
- iOS keyboards cannot open Settings or the containing app. Every «Открыть настройки» and every
  purchase button lives in the app; the iOS keyboard only explains what to do.
- No public API tells the iOS app whether its keyboard is added or has Full Access. The app shows
  what the keyboard last reported and when; unknown stays unknown.
- The iOS paste permission («Вставка из других приложений») is controlled by the system. The app
  uses `PasteButton` (no prompt) and opens its own Settings page as help; it never claims to grant
  the permission.
- Packs and the 24h pass are digital goods: iOS sells them only through IAP (consumable, and
  non-renewing subscription for the pass), Android only through Play Billing (one-time products;
  Play has no non-renewing subscriptions). Purchased packs never expire. No prices are hardcoded.
- Purchases stay hidden until the products exist in App Store Connect / Play Console, the server
  keys are configured and an admin enables them.

### Rollback
All migrations are additive. Older binaries ignore the new tables and columns. Rolling back the
backend means deploying the previous binary; new tables can be dropped by hand afterwards. Clients
gate every new server feature on `/api/v1/config` `features`, so a client release against an old
server shows the previous behaviour.

## 2. Owner decisions (20.10.2026)
- Recipients: Друг / Клиент / Коллега / Партнёр, with icons, in this order.
- Emoji key in the letters page bottom row right after 123, on both keyboards.
- Strict native look for the whole app (system blue, plain grouped backgrounds, no gradients or red
  accents, no decorative motion). The keyboard key look stays as it is.
- Android: native Material in the same restrained style.
- Welcome bonus: new accounts only. Admins grant manually to anyone else.
- Purchases: build everything, keep it off until configured.
- Support: operators are notified in the admin panel only.

## 3. Contracts

### 3.1 Recipient types
Wire and storage values do not change: `friend`, `client`, `work`, `business`, `custom`.
Old builds keep sending them; the server additionally accepts `colleague` (= `work`) and
`partner` (= `business`).

| raw | ru (chip / «Кому отвечаем?») | kk | en | uz | iOS symbol | Android icon |
|---|---|---|---|---|---|---|
| friend | Друг / Другу | Дос / Досқа | Friend | Doʻst | person.2 | Outlined.Group |
| client | Клиент / Клиенту | Клиент / Клиентке | Client | Mijoz | person | Outlined.Person |
| work | Коллега / Коллеге | Әріптес / Әріптеске | Colleague | Hamkasb | briefcase | Outlined.BusinessCenter (Work) |
| business | Партнёр / Партнёру | Серіктес / Серіктеске | Partner | Hamkor | handshake if the SDK has it, else person.line.dotted.person | Outlined.Handshake |

Built-in order for new installs: friend, client, work, business. Existing users keep their stored
order. The keyboard's selected persona (`shared.lastTemplateID`) is the app's selected recipient
too: both read and write it. Icons never change chip height or the position of ✨ and 🎙; when four
chips with icons do not fit, icons are dropped before the row scrolls.

Prompt: `relationshipHints` keep their keys; texts set the register (client and partner formal
Вы/Сіз; friend informal; colleague mirrors) and never choose a language. `PromptVersionReply`
becomes `reply_v3`.

### 3.2 Usage and balances
`usage` (in `/me`, `/me/usage`, session responses and every reply/compose response) keeps all
existing fields with unchanged meaning (`remaining_today` is still plan-only) and adds:

```json
"welcome": {"total": 50, "remaining": 35},
"credits": 0,
"pass": {"expires_at": "2026-10-21T15:00:00+05:00", "remaining": 40},
"remaining_total": 42,
"bucket": "welcome"
```
- `welcome` is omitted when the account has no welcome grant; `pass` when no pass is active.
- `credits` = admin grants + purchased packs remaining. `remaining_total` = plan remaining + pass +
  welcome + credits.
- `bucket` appears only in reply/compose responses: `daily | pass | welcome | admin | pack`.
- Consumption order: plan daily allowance, then active pass, then welcome, then admin grants, then
  packs (oldest first). A monthly plan cap never blocks extra buckets.

Limit errors keep codes `DAILY_LIMIT_REACHED` / `MONTHLY_LIMIT_REACHED` and add
`details.welcome_remaining`, `details.credits_remaining`, `details.pass_active`, `details.can_buy`.

Welcome grant: once per account (`kind='account'` users created after the release cutoff), at
provisioning with a lazy repair on `/me` and session responses. Default units come from the admin
setting `welcome_bonus_units` (initial 20, set in the admin panel, 0 = off). Reinstall, re-login and platform switch never
grant again. Deleting the account and signing up again creates a new account; no identity hash is
kept after deletion (privacy), which is a known limitation.

### 3.3 Idempotent generation
`POST /api/v1/ai/reply` and `/api/v1/ai/compose` accept header `Idempotency-Key`
(`^[A-Za-z0-9-]{8,64}$`). Clients create one key per user action (Reply, Create, Regenerate) and
reuse it only for an automatic retry of that same action.
- Same key after a success: no second charge; the server generates again once (within 10 min) and
  returns `bucket` of the original charge. A further replay: 409 `CONFLICT`,
  `details.field = idempotency_key`, `details.reason = used`.
- Same key while the first request runs: 409, `reason = in_progress`.
- A failed generation is refunded; the same key may then be used again.
- Generated text is never stored to serve replays.
- Feature flag: `idempotent_generation`.

### 3.4 Permission diagnostics
`PUT /api/v1/installations/{installation_id}/diagnostics` (same limiter and optional auth as the
installation endpoints). Strict body:

```json
{
  "keyboard_setup_status": "verified_enabled",
  "keyboard_selected_status": "unsupported",
  "full_access_status": "not_verified",
  "clipboard_status": "unknown",
  "onboarding_status": "in_progress",
  "status_source": "app_check",
  "last_successful_paste_at": null,
  "last_keyboard_check_at": "2026-10-20T10:00:00Z"
}
```
Statuses: `unknown | not_verified | verified_enabled | observed_denied | user_reported_disabled |
unsupported`. Onboarding: `unknown | not_started | in_progress | completed | skipped`.
Source: `app_check | keyboard_check | user_report`.

Meaning per platform:
- iOS `keyboard_setup_status`: `verified_enabled` once the keyboard has reported itself (App Group
  heartbeat or Darwin notification); otherwise `not_verified`. `keyboard_selected_status`:
  `unsupported`.
- iOS `full_access_status`: `verified_enabled` from a Full Access heartbeat; `observed_denied` when
  the keyboard was last seen in limited mode; otherwise `not_verified`.
- iOS `clipboard_status`: `verified_enabled` after a successful user-initiated paste;
  `observed_denied` after a paste where `hasStrings` was true and the read returned nil.
- Android `keyboard_setup_status`: `verified_enabled` when AI Reply is in the enabled input
  methods, else `observed_denied`. `keyboard_selected_status`: `verified_enabled` when it is the
  default input method, else `observed_denied`. `full_access_status`: `unsupported`.
  `clipboard_status`: `verified_enabled` after a successful user-initiated paste, else `unknown`.
- Never any clipboard text, only enums and timestamps. Clients report on foreground when the
  values changed or once per 24 h, and only when `features.permission_diagnostics` is true.
- The response echoes the stored values plus `notification_authorization_status` (derived from the
  installation) and `updated_at`.

### 3.5 Deep links
New `aireply://` screens: `onboarding`, `permissions` (the «Настройка клавиатуры и разрешений»
section), `paste_help`, `support`, `purchase`. Old builds open Home for them. `keyboard` keeps
working and opens the same permissions section in new builds.

### 3.6 Reminders
Automatic types: `onboarding_incomplete`, `keyboard_setup_incomplete`, `clipboard_help`,
`support_reply`, `account_info` (admin-sent only). Category `system` (support reply: `account`).
Evidence comes only from fresh diagnostics (≤ 14 days, source app/keyboard check); unknown or stale
state never triggers a reminder. Cooldowns, max sends, minimum account age and quiet hours are
admin settings; reminders are off by default (`reminders.enabled = false`). A verified success
resolves the reminder for good. Support reply push: one per unread burst, data key
`conversation_id`, link `aireply://support`.

### 3.7 Support chat (user API, `features.support_chat`)
- `GET /api/v1/support/conversation` → `{"conversation": {"id", "status": "open|resolved",
  "unread_count", "created_at", "updated_at"} | null}`
- `GET /api/v1/support/messages?before=<id>|after=<id>&limit=30` (max 100) → `{"messages": [{"id",
  "sender": "user|operator", "text", "created_at", "read_at"}], "has_more"}`, always ascending by
  time. `operator_name` is never exposed; the app shows «Поддержка AI Reply».
- `POST /api/v1/support/messages` `{"text", "client_message_id"}` → 201 `{"message"}`. Text 1–2000
  characters. `client_message_id` (UUID) makes retries idempotent. Writing to a resolved
  conversation reopens it.
- `POST /api/v1/support/read` `{"up_to_message_id"}` → `{"unread_count"}`.
- Apps poll `after=` every 5 s while the chat is open and refresh on a `support_reply` push.

### 3.8 Purchases (`features.iap_ios`, `features.iap_android`)
- `GET /api/v1/iap/products?platform=ios|android` → `{"products": [{"code", "kind": "pack|pass",
  "units", "duration_hours", "store_product_id", "name"}], "account_token": "<uuid>"}`. Only
  products an admin configured (units and store id) and enabled are listed.
- iOS: `product.purchase(options: [.appAccountToken(UUID(account_token))])`, then
  `POST /api/v1/iap/apple/transactions {"signed_transaction": jwsRepresentation}`; call
  `transaction.finish()` only after 200. Re-post `Transaction.unfinished` on launch and listen to
  `Transaction.updates`.
- Android: `setObfuscatedAccountId(account_token)`, then
  `POST /api/v1/iap/google/purchases {"product_id", "purchase_token"}`. The client never consumes;
  the server consumes after granting. Re-post unconsumed purchases from `queryPurchasesAsync` on
  launch.
- Responses: 200 `{"status": "granted|already_granted", "usage": {...}}`; 202 `{"status":
  "pending"}` (Google pending); errors `PURCHASE_INVALID` (400, `details.reason`),
  `PURCHASE_BELONGS_TO_ANOTHER_ACCOUNT` (409), `IAP_UNAVAILABLE` (403).
- Server notifications: `POST /api/v1/iap/apple/notifications` (App Store Server Notifications V2,
  JWS verified), Google voided purchases polled hourly. Refunds revoke what is left; balances never
  go negative.
- A pass grants `units` generations for `duration_hours` from purchase (or from the end of the
  current pass). Packs never expire.

### 3.9 Exhausted sheet
When a generation fails with a limit code: title «Лимит ответов исчерпан», text «Ты использовал
доступные ответы. Можешь получить дополнительные ответы или выбрать другой тариф.», actions
«Докупить ответы» (only when the platform's IAP flag is on and `can_buy`), «Посмотреть тарифы»,
«Позже». The iOS keyboard tells the user to open AI Reply; the Android keyboard can open the app's
purchase screen.

### 3.10 Emoji page
- Entry: an emoji key right after 123 in the letters page bottom row, keyboard key style unchanged.
  Not shown in email, URL, password/secure fields or pads.
- iOS with the in-keyboard globe (home-button phones): the emoji key is dropped when the space bar
  would get narrower than 2.5 grid units; the system globe still reaches the system emoji keyboard.
- The page keeps the exact keyboard height of the letters page. Bottom bar: ABC, category icons,
  delete. Recents first, stored only on the device (never App Group sync or backup). Same curated
  catalog on both platforms; unsupported glyphs are filtered (iOS 17 base: up to Emoji 15.0;
  Android: `Paint.hasGlyph`). Delete removes whole grapheme clusters.

## 4. Migrations
Numbers are fixed so parallel work never collides: `0015_credits.sql`,
`0016_permission_diagnostics.sql`, `0017_support_chat.sql`, `0018_iap.sql`. Additive only, each with a header
(Kazakh summary line, English rationale, rollback note) and a migration test.

## 5. Progress
Work runs in three streams (backend, iOS, Android); each step is committed separately and pushed to
`origin/main` once its tests pass. Backend deploy and store builds are done by the owner.

| Step | State |
|---|---|
| Audit and contracts | done |
| Shared emoji catalog (`tools/emoji`) | done |
| Backend: recipient register rules, colleague/partner aliases, landing names | done |
| Backend: welcome bonus, credit ledger, idempotent generation, admin balances | done |
| Android keyboard: recipient labels/icons, clipboard fixes, limit message, emoji page | done |
| iOS keyboard: recipient labels/icons, paste denial, status evidence, limit message, emoji page | done |
| Backend: permission diagnostics, reminders, push fixes | done |
| Backend: support chat (user + admin) | done |
| Backend: purchases (StoreKit 2 / Play verification, products, admin) | done |
| iOS / Android app: strict native style, onboarding, «Создать ответ», «Подключи AI Reply», permissions section | done |
| iOS / Android app: balance, exhausted sheet, purchases, support chat, diagnostics reporting | done |
| End-to-end check against a local backend, final report (`docs/product-overhaul-report.md`) | done |
