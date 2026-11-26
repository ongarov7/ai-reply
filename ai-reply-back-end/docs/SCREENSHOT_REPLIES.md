# Screenshot replies

The iPhone beta feature was retired on 20 November 2026. The deployed server does
not register these routes. Existing usage and billing history remains available.

A reply to the conversation in a screenshot. The iPhone recognises the chat
itself (Apple Vision) and sends only the text, or — after a separate consent —
the screenshot itself for analysis. Code: `internal/screenshot` (request model,
limits, credit policy), `internal/ai/screenshot.go` (prompt, estimate, reply),
`internal/transport/api/screenshot_handlers.go`.

## API

Both endpoints need a signed-in user with the current legal consent
(`CONSENT_REQUIRED` otherwise). `features.screenshot_reply` and
`features.screenshot_image` in `/api/v1/config` say what the server takes;
`config.screenshot` carries the limits, the policy version, the consent
version and the shortcut link.

### `POST /api/v1/ai/screenshot/estimate`

Nothing leaves the server: the prompt is counted locally, an image is priced by
its size.

```json
{
  "mode": "text",                     // or "image"
  "conversation": {
    "app": "whatsapp", "title": "Aigerim",
    "messages": [
      {"id": "1", "role": "other", "sender": "Aigerim", "text": "Are you free tomorrow?", "time": "12:30"},
      {"id": "2", "role": "self", "text": "Not sure yet."},
      {"id": "3", "role": "other", "text": "We really need your help.", "quote": "…"}
    ],
    "target_id": "3"                   // optional; default: the other side's latest message
  },
  "image": {"width": 1024, "height": 2218},   // image mode: the size only
  "instruction": "Say yes after 7 PM", "instruction_source": "voice",
  "template_id": "friend", "template": {…}, "profile": {…},
  "language": "ru", "input_language": "ru", "platform": "ios", "app_version": "1.4.0"
}
```

`role` is `self`, `other` or `unknown`. Limits: 60 messages, 1 500 characters a
message, 8 000 in all (`400 INVALID_REQUEST` with `field` and `max`).

Answer: `estimated_units`, `max_units`, `unlimited`, `available_units`,
`images_left_today` (Pro, image mode), `input_tokens`, `image_tokens`,
`max_output_tokens`, `tokens_counted`, `reference_tokens`, `output_weight`,
`policy_version`.

### `POST /api/v1/ai/screenshot/reply`

The same body plus:

- `max_units` — the most the person approved (absent: 1);
- `image.data` — base64 JPEG or PNG, at most 3 MiB, sides 64–4096 px (image mode);
- `image_consent` — `legal.ScreenshotConsentVersion` (image mode);
- header `Idempotency-Key` — one charge per user action, as on `/ai/reply`.

Answer: `reply`, `detected_language`, `usage` (as `/ai/reply`), `units` (the
final charge; 0 for a free replay), `refunded_units`, `quote`,
`context_applied`.

Errors besides the usual ones:

| Code | Status | Meaning |
|---|---|---|
| `CREDIT_CONFIRMATION_REQUIRED` | 409 | It may cost more than `max_units`; `details` is the quote. Nothing charged or sent. |
| `INSUFFICIENT_CREDITS` | 402 | The balance cannot pay the maximum; `details` has the quote and `can_buy`. Nothing charged. |
| `SCREENSHOT_CONSENT_REQUIRED` | 403 | Image mode without the current consent version. |
| `SCREENSHOT_IMAGE_UNAVAILABLE` | 403 | Image analysis is switched off in the admin panel. |
| `SCREENSHOT_IMAGE_LIMIT_REACHED` | 429 | Pro used today's image analyses; `details.daily_limit`, `resets_at`. |

## Credit policy (`screenshot-credits-v1`)

One credit is one standard reply: the prompt of an ordinary reply plus a full
400-character answer. A screenshot reply is charged by the tokens it really
used, in those standard replies:

```
w         = output price ÷ input price   (model_pricing row; admin value without one)
weighted  = input_tokens + w × output_tokens
reference = ref_input_tokens + w × ref_output_tokens
credits   = clamp(ceil(weighted ÷ reference − grace), 1, max_units)
```

Input includes the conversation, the image and the account AI context, so
nothing is charged twice (a screenshot reply has no separate AI-context
surcharge). `grace` 0.5 rounds to the nearest credit; 0 always rounds up.

1. Before the call the server computes the most it can cost: its own generous
   count of the prompt (image mode: the provider's input-token count when it
   answers, else the 512-px tile formula) plus the full output limit. If that
   is above `max_units` the answer is `CREDIT_CONFIRMATION_REQUIRED`.
2. It reserves that maximum, all or none, in the usual bucket order (plan
   allowance, pass, welcome, admin, packs): one `screenshot`/`screenshot_image`
   reservation with the key and `screenshot_extra` rows without one.
3. After the call it computes the credits from the provider's real usage
   (both calls if the reply was repaired), commits that many, never more than
   reserved, and refunds the rest. A failed call refunds everything.
4. Pro (unlimited) pays 1. Image analysis on Pro is capped per day
   (`unlimited_images_per_day`).

Examples with the defaults (reference 1 500 + 180 tokens, w = 4 → 2 220):

| Request | Input | Output | Weighted | Credits |
|---|---|---|---|---|
| Ordinary reply size | 1 500 | 180 | 2 220 | 1 |
| Text, 10 messages | 2 000 | 150 | 2 600 (1.17) | 1 |
| Screenshot image | 3 300 | 150 | 3 900 (1.76) | 2 |
| Big image, long answer | 6 000 | 180 | 6 720 (3.03) | 3 |

So 100 credits buy about 100 replies from recognised text and 40–50 replies
with the screenshot itself.

Not charged: capture, cropping, on-device recognition, typing or dictating the
instruction, an estimate, cancelling before generation, a failed generation, a
free replay of a paid key.

### Admin settings

Settings → «Ответы по скриншоту: кредиты» (`system_settings`, audited as
`settings.screenshot_policy.update`):

| Setting | Default | Range |
|---|---|---|
| `screenshot_ref_input_tokens` | 1500 | 200–20 000 |
| `screenshot_ref_output_tokens` | 180 | 32–4 000 |
| `screenshot_output_weight_x100` | 400 | 100–3 000 (only without a price row) |
| `screenshot_rounding_grace_percent` | 50 | 0–90 |
| `screenshot_max_units` | 10 | 1–50 |
| `screenshot_typical_output_percent` | 70 | 10–100 (the estimate's answer length) |
| `screenshot_image_enabled` | 1 | 0/1 |
| `screenshot_image_base_tokens` | 85 | 0–5 000 |
| `screenshot_image_tile_tokens` | 170 | 1–5 000 |
| `screenshot_unlimited_images_per_day` | 50 | 0–1 000 (0 = no cap) |
| `screenshot_shortcut_url` | "" | an `https://www.icloud.com/shortcuts/…` link |

The card also shows what standard replies and screenshot replies really used in
the last 30 days, to calibrate the reference from production numbers.

## Privacy

Neither the screenshot, the recognised text, the instruction nor the reply is
written to the database, the logs or the usage event. The usage event keeps
numbers only: tokens (cached and reasoning included), mode, units, the
approved maximum and the policy version (migration 0043). The image goes to
OpenAI only in image mode, after the in-app consent, with `store: false`.

## Deploy

1. Migration `0043_screenshot_replies.sql` is additive (apply it first if
   `DB_MIGRATE_ON_START=false`).
2. Make sure the production model has a `model_pricing` row: its
   output ÷ input price is the weight `w`.
3. After the iOS build with the screenshot shortcut is installed on an iPhone,
   build the shortcut once, share it, and paste the iCloud link into the admin
   setting.
