# API

Базовый префикс — `/api/v1`. Все ответы — JSON, ошибки в одном конверте:

```json
{ "error": { "code": "DAILY_LIMIT_REACHED", "message": "Daily generation limit reached.",
             "details": { "daily_limit": 7, "used_today": 7, "resets_at": "2026-03-11T19:00:00Z" } } }
```

Клиент реагирует на `code`, а не на текст: `message` — только для отладки,
локализация живёт в приложении.

## Коды ошибок

`INVALID_REQUEST`, `INVALID_OTP`, `OTP_EXPIRED`, `UNAUTHORIZED`, `TOKEN_EXPIRED`,
`ACCOUNT_DISABLED`, `DAILY_LIMIT_REACHED`, `MONTHLY_LIMIT_REACHED`,
`SUBSCRIPTION_EXPIRED`, `RATE_LIMITED`, `AI_PROVIDER_UNAVAILABLE`, `AI_TIMEOUT`,
`AI_EMPTY_RESPONSE`, `PAYMENT_REQUIRED`, `NOT_FOUND`, `CONFLICT`, `INTERNAL_ERROR`.

Вход по почте и через Google/Apple:

| Код | HTTP | Когда | `details` |
|---|---|---|---|
| `INVALID_EMAIL` | 400 | адрес не прошёл нормализацию | — |
| `INVALID_OTP` | 400 | неверный код | `attempts_remaining` |
| `OTP_EXPIRED` | 400 | код старше 5 минут | — |
| `OTP_ALREADY_USED` | 400 | код уже использован или заменён новым | — |
| `OTP_ATTEMPTS_EXCEEDED` | 429 | 5 неверных попыток — нужен новый код | — |
| `OTP_RESEND_COOLDOWN` | 429 | новый код раньше чем через 32 с | `retry_after_seconds` (+ заголовок `Retry-After`) |
| `EMAIL_DELIVERY_FAILED` | 503 | Resend не принял письмо | — |
| `EMAIL_ALREADY_IN_USE` | 409 | адрес принадлежит другому аккаунту | — |
| `INVALID_ID_TOKEN` | 401 | Google/Apple токен не прошёл проверку (это **не** истёкшая сессия: refresh не нужен) | — |
| `AUTH_PROVIDER_UNAVAILABLE` | 503 | ключи Google/Apple недоступны или провайдер не настроен | — |

## Аутентификация

Способы входа: iOS — Apple, Google, почта; Android — Google, почта. Вход по
номеру телефона (WhatsApp/SMS) из приложений удалён; старые эндпоинты оставлены
только для уже установленных сборок (см. ниже). Каждый успешный вход отвечает
одной и той же сессией: `access_token`, `refresh_token`, `expires_in`,
`device_id`, `is_new_user`, `user` (с `auth_providers`), `profile`,
`subscription`, `usage`, `legal_consent`.

`device` во всех запросах входа:
```json
{ "device_id": "", "platform": "ios", "app_version": "1.2.0",
  "os_version": "18.2", "locale": "kk", "timezone": "Asia/Almaty" }
```
`device_id` можно не присылать — сервер вернёт сгенерированный, его нужно
сохранить и присылать дальше.

### POST /api/v1/auth/email/otp/request
```json
{ "email": "Aigerim@Mail.kz", "locale": "kk" }
```
→ `{ "masked_email": "a***m@mail.kz", "expires_in": 300, "resend_after": 32, "code_length": 4 }`

- Адрес нормализуется одной функцией (`auth.NormalizeEmail`: trim, lower-case,
  RFC 5322 без display name, ≤ 254 символов).
- Код — ровно 4 цифры из `crypto/rand` (ведущие нули сохраняются), в БД только
  HMAC-SHA256; живёт 5 минут (`OTP_TTL`), 5 попыток (`OTP_MAX_ATTEMPTS`).
- Новый код аннулирует предыдущий. Повторный запрос раньше `OTP_RESEND_COOLDOWN`
  (32 с) → `OTP_RESEND_COOLDOWN` с `retry_after_seconds`.
- Лимиты: `RATE_OTP_REQUEST_PER_HOUR` на IP и на адрес, `RATE_OTP_REQUEST_PER_DAY` на адрес.
- Ответ одинаков для известного и нового адреса — перечислить аккаунты нельзя.
- Письмо уходит через Resend (HTML + plain text, язык из `locale`). Если Resend
  отказал, код сразу гасится и приходит `EMAIL_DELIVERY_FAILED`; ждать 32 с не нужно.

### POST /api/v1/auth/email/otp/verify
```json
{ "email": "aigerim@mail.kz", "code": "0384", "device": { … } }
```
→ сессия. Аккаунт создаётся при первом входе. Проверка атомарна: код, введённый
одновременно с двух устройств, срабатывает ровно один раз (второе получает
`OTP_ALREADY_USED`). Ошибки: `INVALID_OTP` (+ `attempts_remaining`), `OTP_EXPIRED`,
`OTP_ALREADY_USED`, `OTP_ATTEMPTS_EXCEEDED`, `RATE_LIMITED`.

### POST /api/v1/auth/google
```json
{ "id_token": "eyJ…", "nonce": "<тот же nonce, что передан в Google>", "device": { … } }
```
Сервер проверяет подпись по JWKS Google (`RS256`, кэш по `Cache-Control`),
`iss` (`accounts.google.com`), `aud` (`GOOGLE_CLIENT_ID_IOS` или
`GOOGLE_CLIENT_ID_WEB`), `exp`/`iat`, `sub` и `nonce`. Ошибки:
`INVALID_ID_TOKEN`, `AUTH_PROVIDER_UNAVAILABLE`, `EMAIL_ALREADY_IN_USE`, `ACCOUNT_DISABLED`.

### POST /api/v1/auth/apple
```json
{ "identity_token": "eyJ…", "nonce": "<сырой nonce>", "full_name": "Aigerim S.", "device": { … } }
```
В запрос к Apple уходит `SHA256(nonce)` в hex, серверу — сырой `nonce`; сервер
сверяет хэш с claim `nonce`. `iss` = `https://appleid.apple.com`, `aud` =
`APPLE_CLIENT_ID` (bundle id). `full_name` Apple отдаёт только при первом входе —
сохраняется, только если имя в профиле пустое.

### Как связываются аккаунты

- Ключ личности — `(provider, provider_subject)` в `auth_identities`,
  `UNIQUE(provider, subject)`: повторный вход с тем же Google/Apple `sub`
  всегда попадает в тот же аккаунт, даже если почта у провайдера изменилась.
- Новый `sub` привязывается к существующему аккаунту по почте **только** если
  провайдер является источником истины для этого адреса: Google — `@gmail.com`
  или корпоративный домен с claim `hd`; Apple — `@icloud.com`, `@me.com`,
  `@mac.com` и private relay. Почта при этом должна быть подтверждена провайдером
  (`email_verified`).
- Иначе, если адрес уже занят другим аккаунтом, — `EMAIL_ALREADY_IN_USE`:
  пользователь входит по коду на эту почту и больше ничего не теряет.
- Без подтверждённой почты новый аккаунт не создаётся (`INVALID_ID_TOKEN`).

### POST /api/v1/auth/request-otp · POST /api/v1/auth/verify-otp (устаревшие)

Прежний вход по `identifier` (телефон или почта) для уже установленных сборок.
Почтовый `identifier` идёт тем же путём, что `/auth/email/otp/*` (Resend, 4 цифры,
HMAC, лимиты). Новые сборки их не вызывают.

### POST /api/v1/auth/refresh
`{ "refresh_token": "…" }` → новая пара. Старый refresh сразу отзывается;
повторное использование отзывает всю цепочку сессии (признак кражи токена).

### POST /api/v1/auth/logout
`{ "refresh_token": "…" }` → `{ "ok": true }` (идемпотентно).

## Профиль и лимиты

| Метод | Путь | Описание |
|---|---|---|
| GET | `/api/v1/me` | профиль + тариф + квота одним запросом |
| PATCH | `/api/v1/me` | частичное обновление профиля (все поля опциональны; `POST` — то же для Android) |
| GET | `/api/v1/me/usage` | `daily_limit`, `used_today`, `remaining_today`, `resets_at` |
| GET | `/api/v1/me/subscription` | текущий тариф и статус |
| GET | `/api/v1/me/devices` | список устройств |
| POST | `/api/v1/devices` | регистрация устройства и push-токена |
| DELETE | `/api/v1/devices/{id}` | отозвать устройство |
| GET | `/api/v1/plans` | активные тарифы (публично) |
| GET | `/api/v1/config` | лимиты, языки, режимы, `features.email_otp` / `google_sign_in` / `apple_sign_in` / `compose` / `reply_preferences` / `sender_profile` / `instruction_polish` / `product_events` (публично) |
| POST | `/api/v1/me/email/otp/request` | код на почту, которую нужно добавить к аккаунту (для старых аккаунтов «только телефон») |
| POST | `/api/v1/me/email/otp/verify` | `{ "email", "code" }` → почта привязана, ответ как у `GET /me`; `CONFLICT`, если почта у аккаунта уже есть; `EMAIL_ALREADY_IN_USE`, если адрес занят |

### Профиль: род отправителя и версия онбординга

`profile` (в `GET /me`, в ответе входа и в `/me/email/otp/verify`) содержит, кроме
прежних полей, `grammatical_gender` (`male` | `female` | `unspecified`) и
`onboarding_version` (целое). Клиенты декодируют оба поля как необязательные.

`PATCH /api/v1/me` (и `POST` для Android) принимает их же — только когда сервер
объявил `features.sender_profile = true` (тело читается с `DisallowUnknownFields`,
старый сервер ответит 400):

```json
{ "grammatical_gender": "female", "onboarding_version": 2 }
```

- `grammatical_gender` — только `male`, `female`, `unspecified`; иное → `400 INVALID_REQUEST`.
  Нужен только для русской грамматики («рад/рада», «сделал/сделала»): никогда не
  угадывается по имени, почте или аккаунту, не пишется в логи, события и admin JSON.
  По умолчанию у всех `unspecified` (миграция `0009`) — ответы нейтральны.
- `onboarding_version` — `0…1000`, хранится максимум из старого и нового: устройство
  со старой версией не откатит значение. Вне диапазона → `400`. Миграция `0009`
  ставит `1` тем, у кого `onboarding_completed = true`.
- Тело без новых полей (старые клиенты) их не меняет.

## Генерация ответа

### POST /api/v1/ai/reply
```json
{
  "source_text": "Здравствуйте! Сколько стоит доставка?",
  "instruction": "Ответь вежливо, скажи что уточню",
  "language": "ru",
  "template_id": "client",
  "template": { "name": "Клиент", "relationship": "client", "tone": "professional",
                "instructions": "", "reply_length": "short", "emoji_policy": "minimal",
                "working_hours_behaviour": "mention_when_relevant" },
  "business_context": { "enabled": true, "is_within_working_hours": false,
                        "current_local_time": "22:40", "next_working_period": "завтра 10:00" },
  "input_language": "ru",
  "profile": { "grammatical_gender": "female" },
  "platform": "ios", "app_version": "1.2.0"
}
```
→
```json
{ "reply": "…", "detected_language": "ru",
  "usage": { "daily_limit": 7, "used_today": 1, "remaining_today": 6,
             "resets_at": "2026-03-11T19:00:00Z", "timezone": "Asia/Almaty" } }
```

Порядок на сервере: аутентификация → валидация размера → тариф и квота
(атомарный резерв) → промпт → провайдер → учёт токенов → ответ.
При ошибке провайдера резерв возвращается, счётчик не растёт.

Профиль берётся с сервера; блок `profile` в запросе допускается для клиентов,
которые держат его локально, и имеет приоритет.

Поля под `features.sender_profile` (без флага клиент их не шлёт):

- `input_language` — раскладка клавиатуры в момент нажатия Reply (`kk` | `ru` | `en`).
- `profile.grammatical_gender` — `male` | `female` | `unspecified`; приоритет:
  значение из запроса → сохранённое в профиле → `unspecified`.
- Неизвестные значения этих полей игнорируются (не 400). Неизвестные **ключи** —
  по-прежнему `400 INVALID_REQUEST`.

Язык ответа решает **входящее сообщение**: скопировали сообщение на казахском —
ответ на казахском, даже если телефон, приложение, раскладка и инструкция (или
быстрая кнопка вроде «Ответь согласием.») на русском. Порядок:
`profile.reply_language` (постоянный выбор) → уверенно определённый язык входящего
сообщения → `input_language` → `language` (интерфейс). Последние два решают только
когда язык сообщения не ясен (эмодзи, числа, «ok»). Язык самой `instruction` не
влияет; другой язык даёт только явная просьба в ней («ответь на русском»,
«қазақша жаз», "in English") — такой ответ не исправляется на язык сообщения.
Казахский узнаётся и без казахских букв («Калайсын?», «Кайдасын?»), а сообщение,
где казахские слова смешаны с русскими, считается казахским. Подробно —
[AI_QUALITY.md](AI_QUALITY.md#язык-ответа).

После ответа модели сервер чистит текст (кавычки, «Ответ:», «Вот вариант ответа:»,
markdown, приписка «Примечание: …») и проверяет: род отправителя в русском, язык,
остатки служебного текста. Если проверка нашла проблему, делается **одна**
исправляющая попытка; её результат принимается только если проходит ту же
проверку. Квота списывается один раз, токены обеих попыток учитываются. Ответ
клиенту не меняется: `reply`, `detected_language`, `usage`.

### POST /api/v1/ai/compose

Второй режим клавиатуры — «Создать»: пользователь описывает, что нужно написать,
и получает готовое сообщение. Это не ответ: `source_text` нет, буфер обмена не
участвует, промпт свой (без блока `incoming_message` и правил ответа).

```json
{
  "instruction": "Поздравь директора Сакена Бакпакбековича с 55-летием. Тепло, добавь эмодзи.",
  "language": "ru",
  "regenerate": false,
  "platform": "ios", "app_version": "1.3.0"
}
```
→
```json
{ "text": "Уважаемый Сакен Бакпакбекович! …", "detected_language": "ru",
  "usage": { "daily_limit": 7, "used_today": 2, "remaining_today": 5,
             "resets_at": "2026-03-11T19:00:00Z", "timezone": "Asia/Almaty" } }
```

- Язык сообщения — язык инструкции (или тот, что в ней назван: «на казахском»).
  Если язык инструкции не ясен (только имена или эмодзи) — `input_language`, затем
  `language` (язык интерфейса; он же — метаданные).
- Под `features.sender_profile`: `input_language` (`kk` | `ru` | `en`) и
  `profile: { "grammatical_gender": … }` — в `profile` нет других полей. Сервер
  загружает сохранённый профиль: значение из запроса → сохранённое → `unspecified`.
- `regenerate: true` — тот же запрос, другая версия текста.
- Длина инструкции — `max_instruction_length` из `/api/v1/config` (по умолчанию 400).
  Длиннее — `400 INVALID_REQUEST` с `details { field: "instruction", max_characters: N }`;
  пустая — `details { field: "instruction" }`. Не обрезается молча.
- Квота общая с `/ai/reply`: одна генерация (и каждое «Заново») — одна единица
  дневного и месячного лимита; те же коды `DAILY_LIMIT_REACHED`, `RATE_LIMITED`,
  `AI_TIMEOUT`, `AI_PROVIDER_UNAVAILABLE`, `AI_EMPTY_RESPONSE`; при ошибке провайдера резерв возвращается.
- Лимит токенов ответа — не меньше 700 (поздравление на казахском или русском
  длиннее короткого ответа), но не больше 1024; если в админке задано больше 700, берётся оно.
- Ни инструкция, ни текст не сохраняются: в `ai_usage_events` пишется
  `mode = 'compose'` и длина инструкции (`source_chars`). Миграция `0006` добавляет
  колонку `mode` со значением по умолчанию `'reply'` — старые записи и старые сборки
  сервера не затронуты.
- `GET /api/v1/config` объявляет `features.compose = true`.
- Та же очистка, проверка и одна исправляющая попытка, что у `/ai/reply`.

### POST /api/v1/ai/polish

Подсказка «чище» для текста, который пользователь пишет **для ассистента**
(инструкция ответа, описание в «Создать»). Это не генерация: квота не
резервируется и не тратится.

```json
{ "text": "ответь ему вежливо я сегодня не могу завтра могу",
  "input_language": "ru", "platform": "ios", "app_version": "1.5.0" }
```
→ `{ "text": "Ответь ему вежливо: я сегодня не могу, завтра могу.", "changed": true }`

- `changed: false` — подсказки нет (текст уже чистый или защитная проверка
  сервера отклонила ответ модели); `text` тогда равен исходному. Клиент ничего не показывает.
- Модель исправляет только опечатки, пунктуацию, заглавные буквы, пробелы и явное
  согласование. Детерминированная проверка после модели отклоняет ответ, если
  изменилось или появилось любое число, ссылка, домен, почта, `@имя`, `#тег`, имя
  с заглавной буквы, текст в кавычках; если длина ушла за 0.6–1.6× исходной;
  сменился язык; появился перенос строки.
- Ошибки: `400 INVALID_REQUEST` (пустой текст — `details { field: "text" }`;
  длиннее `max_instruction_length` — `details { field: "text", max_characters: N }`),
  `401`, `429 RATE_LIMITED` (отдельный лимит `RATE_POLISH_PER_MINUTE`, по умолчанию
  20 в минуту на пользователя — не делит лимит с генерацией), `502` / `504` от провайдера.
  Клиенты игнорируют любые ошибки polish молча.
- Выключается `AI_POLISH_ENABLED=false`: тогда `features.instruction_polish = false`,
  а эндпоинт отвечает `404 NOT_FOUND`.
- Ни текст, ни подсказка не сохраняются и не логируются: в `ai_usage_events`
  пишется `mode = 'polish'`, `prompt_version = 'polish_v1'`, длина и токены
  (токены и стоимость учитываются в `usage_daily` / `usage_monthly`, `used` не растёт).
  Графики «генераций» в админке polish не считают.

### Версии промптов и логи

Каждое событие `ai_usage_events` хранит `prompt_version`: `reply_v2`,
`reply_v2+repair_v1` (принят исправленный ответ), `compose_v2`, `compose_v2+repair_v1`,
`polish_v1`. В admin JSON это поле не выводится. Структурные логи без текста и без рода:
`ai_reply_generated` (mode, prompt_version, target_language, language_source,
latency_ms, токены, repaired, truncated; у polish — changed), `ai_reply_failed`
(mode, prompt_version, code, latency_ms), `ai_reply_repaired` (коды проблем, accepted).
Подробно — [AI_QUALITY.md](AI_QUALITY.md).

## Продуктовые события

### POST /api/v1/analytics/events

События онбординга и настроек из приложений — только для подсчёта. Клавиатуры
ничего не отправляют. Клиент шлёт события, только если видел
`features.product_events = true`; без входа — `401`.

```json
{ "platform": "ios", "app_version": "2.0.1",
  "events": [
    { "name": "onboarding_started", "ts": "2026-10-05T09:30:00Z",
      "props": { "version": 2, "trigger": "auto" } },
    { "name": "keyboard_enabled_detected" }
  ] }
```
→ `{ "accepted": 2, "rejected": 0 }`

- `platform` — `ios` | `android`; `app_version` — версия вида `1.0`, `2.0.1` или
  `2.0.1 (57)` (необязательно);
  в пакете 1–20 событий. Нарушение любого из этого — `400 INVALID_REQUEST` с
  `details { field }` (для `events` ещё `max_events: 20`). Неизвестный ключ верхнего
  уровня — тоже `400`.
- Каждое событие проверяется отдельно по закрытому списку сервера: имя, ключи
  `props` и тип каждого значения. Неподходящее событие отклоняется и считается в
  `rejected`, остальные из пакета сохраняются. Свободный текст сохранить нельзя.
- `ts` — время на устройстве (RFC 3339, необязательно), хранится как `client_ts`.

| Событие | `props` (все необязательны) |
|---|---|
| `onboarding_started` | `version`: 0…1000, `trigger`: `auto` \| `settings` |
| `onboarding_step_viewed` | `step`: `welcome` \| `gender` \| `keyboard` \| `fullAccess` \| `copyReply` \| `practice` \| `done` |
| `onboarding_completed` | `version`: 0…1000, `skipped`: bool |
| `gender_selected` | `source`: `onboarding` \| `settings`, `skipped`: bool (сам род не передаётся) |
| `onboarding_keyboard_step_viewed`, `keyboard_enabled_detected`, `full_access_enabled_detected`, `paste_tutorial_viewed`, `onboarding_practice_completed`, `onboarding_reopened`, `autocorrect_enabled`, `autocorrect_disabled` | — |

- `429 RATE_LIMITED` — отдельный лимит `RATE_EVENTS_PER_MINUTE` (по умолчанию 30
  запросов в минуту на пользователя), не делит лимит с генерацией.
- `PRODUCT_EVENTS_ENABLED=false` выключает приём: `features.product_events = false`,
  эндпоинт отвечает `404 NOT_FOUND`.
- Таблица `product_events` (миграция `0010`) удаляется вместе с аккаунтом.
  В админке `GET /api/v1/admin/dashboard` отдаёт `series.product_events` — число
  событий каждого имени за выбранный период.

## Платежи (demo)

| Метод | Путь |
|---|---|
| POST | `/api/v1/payments/checkout` → `{ "plan_id": "…" }` |
| POST | `/api/v1/payments/{id}/confirm` |

В `PAYMENT_MODE=demo` подтверждение сразу переводит пользователя на тариф.
Реальный эквайринг подключается одной реализацией `payments.Provider`.

## Совместимость со старыми сборками

Пока `LEGACY_API_ENABLED=true` работают эндпоинты прежнего бэкенда — байт в байт:

- `POST /v1/auth/register` — `{ "install_id": "…" }` → `{ "token", "expires_at" }`
- `POST /v1/reply/generate` — прежний формат, включая `keyboard_language`,
  `user_instruction` (Android) и `business_context`; ответ `{ "reply", "detected_language" }`;
  ошибки прежними кодами (`rate_limited`, `message_too_long` + `limit`/`actual`, …).

Отличие одно: запросы теперь учитываются в квоте и статистике. `install_id`
хранится только как HMAC — исходное значение в БД не попадает.

## Админ API

`/api/v1/admin/*` — cookie-сессия + заголовок `X-CSRF-Token` на любые изменения.
`session`, `dashboard`, `users`, `users/{id}`, `users/{id}/status|plan|reset-quota|revoke-sessions`,
`plans` (GET/POST/PATCH/archive), `audit`, `settings`, `settings/pricing`, `notifications`, `locale`.

Ни один admin-эндпоинт не отдаёт текст сообщений — такой функции нет.
`dashboard` → `series.product_events`: `[{ "label": "onboarding_completed", "value": 12 }, …]`.

## Служебное

`GET /healthz` — живость, `GET /readyz` — готовность (пинг БД).
