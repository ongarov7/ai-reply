# API

Базовый префикс — `/api/v1`. Все ответы — JSON, ошибки в одном конверте:

```json
{ "error": { "code": "DAILY_LIMIT_REACHED", "message": "Daily generation limit reached.",
             "details": { "daily_limit": 7, "used_today": 7, "resets_at": "2026-03-11T19:00:00Z" },
             "request_id": "req_4f1c2a9b7d3e5f60" } }
```

Клиент реагирует на `code`, а не на текст: `message` — только для отладки,
локализация живёт в приложении. `request_id` совпадает с заголовком ответа
`X-Request-ID` и строкой в журнале сервера — его стоит показывать в отчёте об ошибке.

## Заголовки клиента

Приложения описывают себя заголовками (все необязательные, старые сборки работают без них):

| Заголовок | Пример | Зачем |
|---|---|---|
| `X-Request-ID` | `req_01J8Z6Q3W6Y8` | корреляция; `[A-Za-z0-9._:-]{8,64}`, иначе сервер выдаст свой |
| `traceparent` | W3C Trace Context | `trace_id` в журнале и в записи об ошибке |
| `X-Platform` | `ios` / `android` | журнал, диагностика, отчёт по версиям |
| `X-App-Version` / `X-App-Build` | `1.3.2` / `142` | то же |
| `X-OS-Version` | `17.5` | то же |
| `X-Installation-ID` | UUID установки | обновляет «последний визит» установки; при выходе отвязывает её |
| `X-Session-ID` | UUID сессии приложения | связывает ошибки API с сессией |

Это только подсказки для журналов и аналитики: права доступа по ним не решаются никогда.
Недопустимые значения молча отбрасываются.

## Коды ошибок

`INVALID_REQUEST`, `INVALID_OTP`, `OTP_EXPIRED`, `UNAUTHORIZED`, `TOKEN_EXPIRED`,
`ACCOUNT_DISABLED`, `DAILY_LIMIT_REACHED`, `MONTHLY_LIMIT_REACHED`,
`SUBSCRIPTION_EXPIRED`, `RATE_LIMITED`, `AI_PROVIDER_UNAVAILABLE`, `AI_TIMEOUT`,
`AI_EMPTY_RESPONSE`, `PAYMENT_REQUIRED`, `NOT_FOUND`, `CONFLICT`, `INTERNAL_ERROR`,
`PUSH_DISABLED` (409: отправка push выключена на сервере или провайдер не настроен).

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
Если пришёл заголовок `X-Installation-ID`, установка сразу отвязывается от аккаунта:
на этот телефон больше не уходят push этого аккаунта.

## Профиль и лимиты

| Метод | Путь | Описание |
|---|---|---|
| GET | `/api/v1/me` | профиль + тариф + квота одним запросом |
| PATCH | `/api/v1/me` | частичное обновление профиля (все поля опциональны) |
| GET | `/api/v1/me/usage` | `daily_limit`, `used_today`, `remaining_today`, `resets_at` |
| GET | `/api/v1/me/subscription` | текущий тариф и статус |
| GET | `/api/v1/me/devices` | список устройств |
| POST | `/api/v1/devices` | регистрация устройства (устаревшее; push-токен — через `/installations`) |
| DELETE | `/api/v1/devices/{id}` | отозвать устройство |
| GET | `/api/v1/plans` | активные тарифы (публично) |
| GET | `/api/v1/config` | лимиты, языки, режимы, `features.email_otp` / `google_sign_in` / `apple_sign_in` / `installations` / `push_notifications` / `telemetry` (публично) |
| POST | `/api/v1/me/email/otp/request` | код на почту, которую нужно добавить к аккаунту (для старых аккаунтов «только телефон») |
| POST | `/api/v1/me/email/otp/verify` | `{ "email", "code" }` → почта привязана, ответ как у `GET /me`; `CONFLICT`, если почта у аккаунта уже есть; `EMAIL_ALREADY_IN_USE`, если адрес занят |

## Установки и push-уведомления

Полное описание — [docs/notifications.md](../../docs/notifications.md).

### POST /api/v1/installations
Регистрирует установку приложения (с входом и без). Вызывается при запуске, после
входа, после выхода, при новом push-токене и при смене разрешения.
```json
{ "installation_id": "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d", "platform": "android",
  "app_version": "1.3.2", "app_build": "142", "os_name": "Android", "os_version": "15",
  "device_model": "SM-S928B", "manufacturer": "samsung", "locale": "ru", "timezone": "Asia/Almaty",
  "notification_permission": "authorized", "notifications_enabled": true,
  "push": { "provider": "fcm", "token": "…", "environment": "production" } }
```
→ `{ "installation_id", "attached", "push_status", "push_available", "notifications_enabled", "preferences" }`

- Аккаунт берётся **только** из access token: с токеном установка привязывается к аккаунту,
  без токена — становится анонимной (и отвязывается от прежнего владельца телефона).
  Недействительный токен → `401`, установка не меняется.
- `notification_permission`: `authorized | provisional | ephemeral | denied | not_determined | unknown`.
- `push.environment` (только iOS): `sandbox | production`; без него сервер берёт `APNS_ENVIRONMENT`.
- Токен хранится зашифрованным (AES-256-GCM) и уникален: при повторе на другой установке
  он переезжает на новую. В ответах и в админке его нет — только отпечаток `fcm:1a2b3c4d`.
- `installation_id` генерирует приложение один раз (UUID) и хранит вне резервных копий.

### POST /api/v1/installations/{installation_id}/detach
Отвязать установку от аккаунта (выход, смена аккаунта). С токеном — только свою,
без токена — как анонимная регистрация. → `{ "ok": true, "detached": true }`.

### GET · PUT /api/v1/me/notification-preferences
`{ "preferences": { "subscription": false, "marketing": false } }` →
`{ "preferences": { "account": true, "subscription": false, "security": true, "system": true, "marketing": false },
   "optional": ["account", "subscription", "system", "marketing"] }`.
`security` отключить нельзя. Выключенная категория не создаёт доставок вовсе.

Автоматические push: успешная оплата, подписка истекает через 3 дня, подписка истекла
(категория `subscription`, каждый — не больше одного раза на событие).

Данные push (`data`): `nid` (уведомление), `did` (доставка), `type`, `category`, `link`
(`aireply://home|subscription|settings|notifications|templates|profile|keyboard|compose`
или `https://` на разрешённом хосте). Приложение проверяет ссылку ещё раз.

## События приложения

### POST /api/v1/events
Пакет до 50 событий, тело до 64 КБ, 202 Accepted (запись в фоне).
```json
{ "installation_id": "0b7c9a52-…", "session_id": "5e55a0b1-…",
  "events": [ { "id": "c1f0…", "name": "notification_opened", "occurred_at": "2026-10-01T09:30:00Z",
                "properties": { "notification_id": "…", "delivery_id": "…" } } ] }
```
→ `{ "accepted": 1, "rejected": 0 }` (при отказе — `rejections: [{ "index", "reason" }]`).

Разрешены только события из списка (`app_opened`, `app_backgrounded`, `login_success`,
`login_failed`, `registration_success`, `logout`, `push_permission_requested|granted|denied`,
`push_token_registered|refreshed|registration_failed`, `notification_opened`,
`payment_started|success|failed`, `api_error`, `unexpected_app_error`) с типизированными
свойствами; остальное отклоняется, а не сохраняется. Текст сообщений, нажатия клавиш,
содержимое буфера и любые введённые данные не принимаются в принципе. Повтор пакета с теми
же `id` не создаёт дублей. `notification_opened` отмечает доставку открытой, только если
пришёл с той же установки.

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

Права проверяются на сервере для каждого маршрута (`403 FORBIDDEN` + недостающее право);
`GET /session` отдаёт список прав текущего администратора. Роль `admin` — все права,
`viewer` — `dashboard.read`, `users.read`, `settings.read`, `notifications.read`.

| Метод | Путь | Право |
|---|---|---|
| GET | `/notifications` | `notifications.read` — состояние push (провайдеры, воркер) |
| POST | `/notifications/audience/preview` | `notifications.read` — число получателей по фильтру (считает сервер) |
| GET · POST | `/notifications/campaigns` | `notifications.read` / `notifications.send`; POST требует `Idempotency-Key` |
| GET | `/notifications/campaigns/{id}` | `notifications.read` — статистика и ошибки доставки |
| POST | `/notifications/campaigns/{id}/send` · `/cancel` | `notifications.send` |
| GET | `/notifications/deliveries` · `/notifications/devices` | `notifications.read` |
| GET | `/users/{id}/diagnostics` | `users.diagnostics.read` (просмотр попадает в аудит) |
| GET | `/logs/events` · `/logs/auth` · `/logs/errors` · `/logs/versions` · `/logs/meta` | `logs.read` |
| GET | `/ops` | `dashboard.read` |
| GET | `/audit?action=&admin=&entity_type=&entity_id=` | `audit_logs.read`; `action` с точкой на конце — префикс |

Фильтр `?user=` в журналах принимает id (или его начало), почту или телефон; поиск по почте
находит и неудачные попытки входа (по ключевому хэшу — сам адрес в журнале не хранится).

Ни один admin-эндпоинт не отдаёт текст сообщений — такой функции нет.

## Служебное

`GET /healthz` — живость, `GET /readyz` — готовность (пинг БД).
