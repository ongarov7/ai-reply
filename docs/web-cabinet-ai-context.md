# Web cabinet and account AI context — specification

Status: agreed with the owner on 2026-11-17. This file is the contract between the
backend, the website cabinet and the apps. Change it only together with the code.

## 1. What we are building

1. **Website cabinet** (`/account`, `/payment`): a proper web version of the account.
   - «Войти» button in the site header; email + 4-digit code sign-in (already exists).
   - Overview: plan, package/balance in detail, a small usage chart, where the user uses AI Reply.
   - **AI profile**: who the user is (role chips, occupation, communication-style chips),
     a free «about me / what I do» text, audience (followers, customers), typed or dictated
     with the microphone.
   - **Recipients**: how the user talks to friends, clients, colleagues and partners
     (one note per recipient), typed or dictated.
   - Top-up/purchase (the existing, tested BCC flow — keep its behaviour), app downloads.
2. **Server-side use**: the saved context is injected by the server into every
   `POST /api/v1/ai/reply` and `POST /api/v1/ai/compose`, for every platform (iOS app and
   keyboard, Android app and keyboard, Apple Watch via the phone, macOS, Windows). Old store
   builds benefit without an update because the server does it.
3. **Billing for context** (owner's decision): a reply costs
   `1 + min(max_extra, ceil(max(0, context_chars − free_chars) / chars_per_reply))` units.
   Defaults: `chars_per_reply = 400`, `free_chars = 0`, `max_extra = 2` (admin-configurable).
   Pro/unlimited pays no surcharge. Role/style chips are free; only typed text counts.
4. **Scope (owner, 2026-11-17): backend and website only. iOS, Android, macOS and Windows
   code is not touched.** The apps get the feature through the server-side injection (§4) and
   show the new balance from the server's `usage` as they do today. The app endpoints in §5.3
   and the handoff `target` exist for future app versions; nothing calls them yet.

## 2. The AI context document

One document per account, stored on the server.

```json
{
  "enabled": true,
  "roles": ["seller", "blogger"],
  "occupation": "Продаю детскую одежду в Instagram",
  "traits": ["brief", "no_emoji"],
  "about": "Магазин детской одежды, доставка по Алматы 1–2 дня…",
  "audience": "Instagram 25 тыс. подписчиков, в основном мамы 25–35 лет",
  "recipients": {
    "friend":    { "notes": "" },
    "client":    { "notes": "Всегда на «Вы», вежливо, предлагаю примерку" },
    "colleague": { "notes": "" },
    "partner":   { "notes": "" }
  }
}
```

| Field | Type | Limit | Notes |
|---|---|---|---|
| `enabled` | bool | — | false → nothing is injected and nothing is charged. Default true. |
| `roles` | string[] | ≤ 5 codes | codes from §2.1; duplicates removed; unknown → 400 |
| `occupation` | string | ≤ 120 chars | free text («кем работаю») |
| `traits` | string[] | ≤ 6 codes | codes from §2.2 |
| `about` | string | ≤ 2000 chars | who I am, what I do, facts |
| `audience` | string | ≤ 300 chars | followers, platforms, customers |
| `recipients.<key>.notes` | string | ≤ 1000 chars | keys exactly `friend`, `client`, `colleague`, `partner` |

"chars" = Unicode code points (`utf8.RuneCountInString` in Go, `Array.from(s).length` in JS,
`unicodeScalars.count` in Swift, `codePointCount` in Kotlin). Text is trimmed by the server;
`\r` is removed; other control characters except `\n` and `\t`, the separators U+2028/U+2029
and bidirectional controls (U+061C, U+200E, U+200F, U+202A–U+202E, U+2066–U+2069) are rejected
(`invalid_text`). Over-limit text is **rejected** (400), never silently cut. Responses always
contain all four recipient keys.

### 2.1 Role codes (labels)

| code | ru | en | kk | uz |
|---|---|---|---|---|
| seller | Продавец | Seller | Сатушы | Sotuvchi |
| business_owner | Владелец бизнеса | Business owner | Бизнес иесі | Biznes egasi |
| entrepreneur | Предприниматель | Entrepreneur | Кәсіпкер | Tadbirkor |
| investor | Инвестор | Investor | Инвестор | Investor |
| partner | Партнёр в бизнесе | Business partner | Бизнестегі серіктес | Biznes hamkori |
| manager | Руководитель | Manager | Басшы | Rahbar |
| employee | Сотрудник | Employee | Қызметкер | Xodim |
| freelancer | Фрилансер | Freelancer | Фрилансер | Frilanser |
| designer | Дизайнер | Designer | Дизайнер | Dizayner |
| artist | Художник | Artist | Суретші | Rassom |
| developer | Разработчик | Developer | Әзірлеуші | Dasturchi |
| marketer | Маркетолог | Marketer | Маркетолог | Marketolog |
| blogger | Блогер | Blogger | Блогер | Bloger |
| influencer | Инфлюенсер | Influencer | Инфлюенсер | Influenser |
| student | Студент | Student | Студент | Talaba |
| teacher | Преподаватель | Teacher | Оқытушы | Oʻqituvchi |
| doctor | Врач | Doctor | Дәрігер | Shifokor |
| lawyer | Юрист | Lawyer | Заңгер | Yurist |
| realtor | Риэлтор | Realtor | Риэлтор | Rieltor |

### 2.2 Communication-style codes (labels)

Opposite pairs are exclusive — `introvert`/`extrovert`, `brief`/`detailed`, `formal`/`casual`,
`emoji`/`no_emoji`: the server refuses both together (`400`, field `traits`, reason `conflict`);
the cabinet unselects the opposite chip when one is chosen.

| code | ru | en | kk | uz |
|---|---|---|---|---|
| introvert | Интроверт | Introvert | Интроверт | Introvert |
| extrovert | Экстраверт | Extrovert | Экстраверт | Ekstrovert |
| brief | Пишу коротко | Brief | Қысқа жазамын | Qisqa yozaman |
| detailed | Пишу подробно | Detailed | Толық жазамын | Batafsil yozaman |
| formal | Официально, на «Вы» | Formal | Ресми, «Сіз» деп | Rasmiy, «Siz» deb |
| casual | Неформально | Casual | Бейресми | Norasmiy |
| humor | С юмором | With humour | Әзілмен | Hazil bilan |
| emoji | Люблю смайлики | Likes emoji | Смайлик қолданамын | Smayliklarni yoqtiraman |
| no_emoji | Без смайликов | No emoji | Смайликсіз | Smayliksiz |
| diplomatic | Дипломатично | Diplomatic | Дипломатиялық | Diplomatik |
| direct | Прямо и по делу | Direct | Тура әрі нақты | Aniq va loʻnda |
| warm | Тепло и заботливо | Warm | Жылы әрі қамқор | Samimiy |

Recipients use the apps' existing names: Друг / Клиент / Коллега / Партнёр (ru),
Friend / Client / Colleague / Partner (en). For kk and uz reuse the strings the iOS/Android
apps already ship for these four recipients (search their string resources), so all
platforms say the same thing.

## 3. Pricing (surcharge for context)

Admin settings (system_settings, admin panel → Settings → «AI-контекст»):

| key | default | range | meaning |
|---|---|---|---|
| `ai_context_chars_per_reply` | 400 | 0–4000 | characters per extra reply; 0 = no surcharge |
| `ai_context_free_chars` | 0 | 0–10000 | characters included in a normal reply |
| `ai_context_max_extra_replies` | 2 | 0–10 | cap on the surcharge per reply |

```
base_chars       = chars(occupation) + chars(about) + chars(audience)
context_chars(r) = base_chars + chars(recipients[r].notes)      // r = none → notes = 0
extra(r)  = 0                                         if !enabled || chars_per_reply == 0 || unlimited
          = min(max_extra, ceil(max(0, context_chars(r) − free_chars) / chars_per_reply))
units(r)  = 1 + extra(r)
```

`unlimited` = the account's current entitlement is unlimited (Pro). Roles and traits never
count. The website and the apps show the user `units(r)` («с вашим контекстом 1 ответ = 3 из
пакета»). The server computes the same numbers and returns them (`cost` in §5.1); clients may
compute them live while the user types with the same formula and the `pricing` object.

### 3.1 How a generation is charged

1. The normal reservation (1 unit) happens first, exactly as today.
2. If the context applies (enabled, non-empty for this request) and `extra > 0` and the paid
   bucket is not `unlimited` and the request is not a free Idempotency-Key replay, the server
   reserves `extra` more units, each as its own reservation (`usage_reservations.mode =
   'context'`, no idempotency key), choosing buckets by the normal order.
3. If any extra unit cannot be reserved (no balance left), the extras already reserved are
   refunded and the reply is generated **without** the account context and recipient notes,
   for 1 unit (`context_applied = false`).
4. Provider failure → every reservation of the request is refunded. Success → all committed.
5. A replay of a paid Idempotency-Key is free. It uses the context only if the key's first
   generation paid its surcharge (or needed none); a key first served by the fallback replays
   without the context.
6. The extra units are reserved all or none in one transaction. Pro pays no surcharge even on
   an extra device that falls back to the account's purchased packs.

## 4. Generation behaviour

- Recipient of a reply: `template_id` if it is one of `friend`, `client`, `work`/`colleague`,
  `business`/`partner` (`work`→`colleague`, `business`→`partner`); otherwise
  `template.relationship` mapped the same way; otherwise none (custom personas). Compose:
  `template_id` mapped the same way when present (macOS sends it), else none.
- The account context is added to the **user** message as data blocks (never to the developer
  message): `<account_context>` (roles, occupation, style, about, audience) and
  `<recipient_notes>` (notes for the recipient). It never changes the reply language, which
  still follows the copied message.
- It is added for every platform, including desktop sessions that send their own local
  `context` (desktop budget checks apply to the desktop's own text only).
- The existing profile fields and the apps' per-request `profile`/`template` overrides keep
  working unchanged; the account context is additional.
- Prompt version gets the suffix `+account_context_v1` when the block is present.
- Response of `/ai/reply` and `/ai/compose` gains two optional fields:
  `units` (int, units charged by this request: 1–3, 0 for a free replay) and
  `context_applied` (bool). Old clients ignore them.

## 5. API

Errors use the existing envelope `{"error": {"code", "message", "details"}}`.

### 5.1 `AIContextResponse` (all GET/PUT/POST below return it)

```json
{
  "user_id": "u_123",
  "context": { ...document from §2 },
  "revision": 3,
  "updated_at": "2026-11-17T10:00:00Z",
  "limits": { "roles": 5, "traits": 6, "occupation": 120, "about": 2000, "audience": 300, "notes": 1000 },
  "catalog": {
    "roles": ["seller", "business_owner", "…"],
    "traits": ["introvert", "…"],
    "recipients": ["friend", "client", "colleague", "partner"]
  },
  "pricing": { "chars_per_reply": 400, "free_chars": 0, "max_extra_replies": 2, "unlimited": false },
  "cost": { "base_chars": 450, "general": 3, "friend": 3, "client": 3, "colleague": 3, "partner": 3 }
}
```

`user_id` names the account the document belongs to: the cabinet never adopts a read answered
for another account than the page shows (it reloads, or keeps unsaved text and blocks saving).
`revision` is 0 and `updated_at` is `""` before the first save (then `context` is the
default document: enabled, everything empty). `cost.general` = units for a request with no
recipient (custom personas, compose without recipient); the other keys = units per recipient.

### 5.2 Save body

```json
{ "context": { ...document from §2 }, "expected_revision": 3, "expected_user_id": "u_123" }
```

`expected_user_id` is the account the page/app shows: **required from the website**
(`/account/api/ai-context`), optional with an app token. A mismatch → `409
WEB_ACCOUNT_CHANGED`, nothing saved (a tab left open after switching accounts).

- `expected_revision` is required. If it differs from the stored revision → `409
  AI_CONTEXT_CHANGED`, `details: {"revision": <current>}`; nothing is saved. Clients reload
  and show «Профиль изменился на другом устройстве».
- Validation → `400 INVALID_REQUEST`, `details: {"field": "about", "reason": "too_long",
  "max": 2000}`; reasons: `too_long`, `too_many` (+`max`), `unknown` (roles/traits/recipient
  key), `invalid_text`, `conflict` (opposite style chips), `required`.
- Success → 200 `AIContextResponse` with `revision + 1`.
- The whole document is replaced (send every field).

### 5.3 App endpoints (Bearer token)

| Method | Path | |
|---|---|---|
| GET | `/api/v1/me/ai-context` | `AIContextResponse` |
| PUT | `/api/v1/me/ai-context` | body §5.2 |
| POST | `/api/v1/me/ai-context` | same as PUT (Android `HttpURLConnection` alias) |

`GET /api/v1/config` adds `features.ai_context: true` and
`"ai_context_pricing": {"chars_per_reply": 400, "free_chars": 0, "max_extra_replies": 2}`.
Clients must not call these endpoints unless `features.ai_context` is true.

### 5.4 Website endpoints (cookie session `aireply_web_session`, path `/account/`)

All POSTs need the existing same-origin headers (`Origin` = site origin,
`X-Requested-With: AIReplyAccount`, `Content-Type: application/json`), except transcribe,
which is `multipart/form-data` with the same `Origin` and `X-Requested-With` headers.

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/account/api/session` | — | existing payload **plus** `features` and `legal` (below) |
| GET | `/account/api/ai-context` | — | `AIContextResponse` |
| POST | `/account/api/ai-context` | §5.2 | `AIContextResponse` |
| GET | `/account/api/usage?days=30` | — | `UsageHistory` (days 7–90, default 30) |
| POST | `/account/api/consent` | `{"terms_version","privacy_version","locale","expected_user_id"}` | `{"legal_consent": {...}}` |
| POST | `/account/api/transcribe` | multipart: `audio` (WAV 16 kHz mono 16-bit or M4A, ≤ 40 s, ≤ 4 MiB), `duration_ms`, `request_id` (`[A-Za-z0-9-]{8,64}`), `languages` (`ru,kk,en`, ≤3), `purpose` (`dictation`), `expected_user_id` | `{"text","languages","duration_ms"}` |

Every cabinet write (`ai-context`, `consent`, `transcribe`) carries `expected_user_id` = the
`user.id` of the session the page shows; a mismatch is `409 WEB_ACCOUNT_CHANGED` (reload).

Session additions:

```json
"features": { "ai_context": true, "voice_transcription": true },
"legal": { "consent_required": false, "terms_version": "…", "privacy_version": "…",
           "terms_url": "https://…/offer", "privacy_url": "https://…/privacy" }
```

`UsageHistory`:

```json
{
  "user_id": "u_123",
  "days": [ { "date": "2026-10-19", "replies": 3, "units": 4 } ],
  "platforms": [ { "platform": "ios", "replies": 12 }, { "platform": "macos", "replies": 3 } ],
  "total_replies": 15,
  "total_units": 18,
  "timezone": "Asia/Almaty"
}
```

`days` has exactly `days` entries, oldest first, today last (server timezone), zeros included.
`replies` = successful generations (reply + compose); `units` = units charged that day
(plan + extra buckets, surcharge included). `platforms` covers the same period, sorted by
replies, values as sent by the clients (`ios`, `android`, `macos`, `windows`, `web`, …).

Transcribe errors: `404 NOT_FOUND` (voice disabled — hide the mic), `403 CONSENT_REQUIRED`
(show the consent dialog, `POST /account/api/consent`, retry), `400 INVALID_REQUEST` (bad
audio: `details.field = "audio"`, `reason` `format|unreadable|too_short|too_long|too_large`),
`409 WEB_ACCOUNT_CHANGED`, `429 RATE_LIMITED` (per minute) and `429 VOICE_LIMIT_REACHED` (the
day's voice cap). Voice spends no replies. The cabinet pages send
`Permissions-Policy: microphone=(self)`; every other page keeps the microphone closed.

Web handoff (apps → website): `POST /api/v1/web/payment-handoff` accepts an optional
`"target": "account" | "payment"` (default `payment`). With `account` the returned URL is
`<origin>/account#handoff=<code>`. Clients send `target` only when `features.ai_context` is true.

## 6. Website cabinet (UX)

Keep: every existing DOM id used by `static/account.js` and its tests, the email/code flow,
the handoff and account-switch flow, the BCC purchase flow (terms checkbox, recurring consent,
order polling) — restyle, do not rewrite that logic. `account.js` may announce state changes
with `document.dispatchEvent(new CustomEvent('aireply:session', {detail: payload}))` and
`'aireply:signedout'` (guarded so the Node tests' fake `document` keeps working).

Layout (signed in): left sidebar on desktop, horizontal scrollable tabs on phones (≤ 760 px,
16 px gutters, no horizontal page scroll). Sections, addressable by hash:

1. `#overview` — greeting with the email; tiles: available replies (∞ for unlimited), plan
   (name, paid until / renews), used in the last 30 days; the usage bar chart (inline SVG,
   30 days, units per day, accessible table/`aria-label`, today highlighted, no library);
   platforms list («Где вы пользуетесь»); a card «Стоимость ответа с вашим AI-профилем:
   1 ответ = N» linking to the profile; quick actions (top up, AI profile, download apps).
2. `#profile` — «AI-профиль»: switch «Использовать при генерации», role chips (multi, ≤5),
   occupation, style chips (≤6), about (textarea, counter, mic), audience (counter, mic);
   live cost line; Save bar that appears when there are unsaved changes; `beforeunload`
   warning; 409 handling.
3. `#recipients` — four cards (Друг, Клиент, Коллега, Партнёр): notes textarea with
   counter, mic and placeholder examples; per-card cost «1 ответ = N».
4. `#billing` — the existing purchase UI (products, terms, renewal, subscriptions, order).
   `/payment` opens this section; signed-out `/payment` keeps the public pack offers.
5. `#apps` — downloads: App Store, Google Play, macOS (`/mac`), Windows (`/windows`), using
   the URLs the landing already uses; hide links that are not configured.
6. Account footer/side: email, account id, sign out, delete account link.

Signed out: a clean centred sign-in card (email → code, resend timer, change email) with a
short list of what the cabinet gives; `/payment` additionally shows the pack offers.

Microphone (web): `getUserMedia` → Web Audio capture → downsample to 16 kHz mono 16-bit
PCM → WAV (browsers' MediaRecorder WebM is **not** accepted by the server) → POST
`/account/api/transcribe`. 40 s hard stop with a visible timer; ≥ 0.3 s; append the text at
the cursor. Hide mics when `features.voice_transcription` is false or the browser lacks
`getUserMedia`/`AudioContext`.

Design: same tokens as `landing.css` (system-blue brand `#3b5bfd`, 8 px radius, white cards on
`--bg-soft`), strict native business look: no new gradients, no red except errors, no flashy
motion (respect `prefers-reduced-motion`), visible focus rings, AA contrast, labels for every
input, `aria-live` for save/recording status. All text through the locale files (`account.*`
keys, all four locales: ru, kk, en, uz). Header: add a «Войти» link/button (`nav.sign_in`)
next to «Скачать» on commercial pages, pointing to `/account?lang=…`; the nav item «Личный
кабинет» points to `/account`.

## 7. Apps (not in this release)

No app code changes now (owner's decision). When an app adds an editor later: gate on
`features.ai_context`; GET on open; save with `expected_revision`; on 409 reload and tell the
user; show the cost per reply from `cost`; never keep the document across accounts; keep the
existing local profile and personas untouched. iOS must stay fully native (no website links,
App Store rules); macOS may open the cabinet with the handoff `target: "account"`.

## 8. Privacy

The document is personal data entered by the user. It is stored on our server (deleted with
the account, cascade) and sent to the AI provider only as part of a generation. It is never
logged. The privacy policy gains one sentence about the account AI profile.

## 9. Deploy order

1. Backend (migration `0042_ai_context.sql`, new settings, endpoints) — the owner deploys.
2. Website ships with the backend (same binary).
3. Apps: no release needed — the server applies the context to every generation.
