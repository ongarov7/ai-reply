# Push-уведомления, установки и телеметрия

Этот документ описывает всю цепочку: от входа в приложение до нажатия на
уведомление, а также настройку Firebase и Apple, переменные окружения, схему
БД, повторы, идемпотентность, журналы и приватность. Детали API — в
[ai-reply-back-end/docs/API.md](../ai-reply-back-end/docs/API.md), детали
клиентов — в разделах «Push notifications» файлов
[AI-Reply-Android/README.md](../AI-Reply-Android/README.md) и
[AI-Reply/README.md](../AI-Reply/README.md).

## 1. Архитектура

```
 Админ-панель (/admin, роли и права)         бизнес-события (оплата, подписка)
        │  кампания + Idempotency-Key                  │  NotifyUser(dedupe key)
        ▼                                              ▼
 ┌────────────────────────── AI Reply backend (Go, SQLite) ───────────────────────┐
 │ notifications.Service ── AudienceResolver (repository/audience.go, только SQL) │
 │        │ notifications (UNIQUE dedupe_key)                                     │
 │        ▼                                                                       │
 │ notification_deliveries — outbox (UNIQUE notification_id + installation_id)    │
 │        │ claim с арендой (lease) → Dispatcher → push.Provider                   │
 │        ├──────────── FCM HTTP v1 (OAuth сервис-аккаунта, RS256)                │
 │        └──────────── APNs HTTP/2 (JWT .p8, ES256)                              │
 │ installations.Service (app_installations, токены зашифрованы)                  │
 │ telemetry.Service (app_events, app_sessions, auth_events, api_errors)          │
 └────────────────────────────────────────────────────────────────────────────────┘
        ▲ /api/v1/installations, /events, /auth/*            │ FCM / APNs
        │                                                    ▼
   Android (FirebaseMessagingService)              iOS (UNUserNotificationCenter)
```

Ключевые правила:

- Ключи провайдеров есть **только** у бэкенда (переменные окружения). В
  приложениях — только клиентская конфигурация Firebase (`google-services.json`,
  не секрет, но в git не кладётся) и entitlement `aps-environment`.
- Бизнес-код не вызывает FCM/APNs: он вызывает `notifications.Service`
  (`NotifyUser`, кампании), сервис пишет уведомление и доставки в БД, отправляет
  только диспетчер.
- Источник правды об аккаунте, подписке, устройстве и доставке — БД бэкенда.

Пакеты бэкенда: `internal/installations` (реестр установок), `internal/push`
(FCM, APNs, классификация ошибок), `internal/notifications` (сервис, валидация,
диспетчер, бизнес-события), `internal/repository` (SQL: installations,
audience, notifications, telemetry), `internal/telemetry` (события, журналы
входа, ошибки API, сроки хранения), `internal/reqctx` (метаданные клиента,
request id), `internal/redact` (маскирование), `internal/admin` +
`internal/transport/adminapi` (права, кампании, диагностика, журналы).

## 2. Полный путь уведомления

1. **Установка.** При запуске (после принятия условий) приложение создаёт
   случайный UUID установки и регистрирует её: `POST /api/v1/installations`
   с метаданными (версия, сборка, ОС, модель, язык, часовой пояс, разрешение,
   внутренний переключатель). Без входа установка анонимная.
2. **Вход.** После входа приложение повторяет регистрацию с access-токеном:
   установка привязывается к аккаунту (только по токену, не по телу запроса).
3. **Токен.** Android получает FCM registration token, iOS — APNs device token
   (только Release-сборка с `aps-environment`); токен уходит той же регистрацией.
   Сервер хранит его зашифрованным (AES-256-GCM) и уникальным: если токен
   пришёл с другой установки, он переезжает, старая получает `push_status=replaced`.
4. **Уведомление.** Админ создаёт кампанию (сервер считает аудиторию сам) или
   бизнес-код вызывает `NotifyUser`. Создаётся одна строка `notifications`
   (уникальный `dedupe_key`) и по одной строке `notification_deliveries` на
   каждую подходящую установку.
5. **Outbox.** Диспетчер (горутина в каждом инстансе) атомарно «арендует»
   пачку доставок (`UPDATE … RETURNING`, аренда 5 минут), отправляет и
   записывает результат только если аренда всё ещё его.
6. **Провайдер.** FCM/APNs отвечают «принято», «повторить позже», «токен
   недействителен» или «отклонено». «Принято провайдером» — это **не**
   «доставлено» и не «прочитано».
7. **Устройство.** В фоне систему показывает уведомление сама; в foreground
   приложение показывает его тем же способом (Android: тот же канал и
   `tag = nid`; iOS: `willPresent` → баннер, список, звук).
8. **Нажатие.** Приложение отправляет событие `notification_opened`
   (`notification_id`, `delivery_id`), сервер ставит `opened_at` у доставки,
   только если событие пришло с той же установки.
9. **Deep link.** `aireply://<экран>` открывает экран приложения после всех
   шлюзов (согласие, вход, онбординг — никогда в обход), `https://ai-reply.kz/...`
   — во встроенном/внешнем браузере, всё остальное просто открывает приложение.

## 3. Бэкенд

### 3.1 Установки и токены (`internal/installations`)

- `installation_id`: `^[A-Za-z0-9-]{8,64}$`, генерирует приложение.
- Платформа `ios|android`; разрешение `authorized|denied|not_determined|provisional|ephemeral|unknown`.
- FCM-токен: 20–4096 символов допустимого алфавита; APNs: hex 64–200, приводится к нижнему регистру;
  окружение APNs `sandbox|production` (по умолчанию — `APNS_ENVIRONMENT`).
- Хранение: `push_token_sealed` (AES-256-GCM, ключ = HMAC(JWT_ACCESS_SECRET, "ai-reply/push-token/v1")),
  `push_token_hash` (SHA-256, уникален в паре с провайдером). В журналах и админке — только
  отпечаток `fcm:1a2b3c4d` / `apns:…`.
- Привязка к аккаунту: регистрация с действительным токеном привязывает, без токена — отвязывает
  (fail-safe), с недействительным — `401` и ничего не меняется (истёкший токен не отвязывает).
- `POST /api/v1/auth/logout` с заголовком `X-Installation-ID` отвязывает установку (даже если
  refresh-токен уже истёк). Отключение пользователя админом и «отозвать сессии» тоже отвязывают
  все его установки.
- `last_seen_at` обновляется не чаще раза в 15 минут на установку (в памяти + SQL-условие).

### 3.2 Уведомления и доставки (`internal/notifications`)

- `NotifyUser(UserNotification{UserID, IdempotencyKey, Type, Category, Title, Body, Link, Data})`
  → `dedupe_key = "user:<uid>:<key>"`. Повтор с тем же ключом не создаёт ничего нового.
- Категории: `account`, `subscription`, `security` (отключить нельзя), `system`, `marketing`.
  Важные (`account`, `subscription`, `security`) идут с высоким приоритетом и в канал Android
  `important`, остальные — в `general`.
- Ограничения: заголовок ≤ 80, текст ≤ 400 символов, data ≤ 10 ключей (`^[a-z][a-z0-9_]{0,31}$`,
  без зарезервированных), значение ≤ 256, data ≤ 1 КБ, полезная нагрузка в пределах лимитов FCM/APNs.
- Ссылки (`ValidateLink`): `aireply://home|subscription|settings|notifications|templates|profile|keyboard|compose`
  без пути и query; либо `https://` на хост из `PUSH_LINK_HOSTS` или его поддомен, без
  userinfo и порта. `javascript:`, `intent:`, `http:` и прочее отклоняются.
- Получатели: установка не старше 270 дней (`StaleAfter`), push активен, внутренний
  переключатель включён, разрешение не `denied`, категория не выключена в настройках
  пользователя, провайдер настроен.
- Автоматические push (`events.go`): успешная оплата (`payment_success:<payment_id>`),
  подписка истекает в ближайшие 72 часа (`subscription_expiring:<sub>:<expires_ms>`), подписка
  истекла за последние 24 часа (`subscription_expired:<sub>:<expires_ms>`). Проверка — каждые
  15 минут, ключи делают повторы безопасными. Тексты — на языке пользователя (en/ru/kk/uz).

### 3.3 Кампании и аудитория

- Фильтр (`domain.AudienceFilter`): `platforms`, `auth` (`authenticated|anonymous`), `payment`
  (`paid|unpaid`), `subscription` (`active|expired|none`), `locales`, `app_version_min/max`,
  `os_version_min`, `active_within_days`, `inactive_for_days`, `registered_from/to`, `user_ids`
  (≤ 500). Всё проверяется на сервере, произвольные поля и SQL не принимаются.
- `POST /api/v1/admin/notifications/audience/preview` только считает (пользователи, устройства,
  Android/iOS, анонимные) одним SQL-запросом, ничего не отправляет; ограничен 30 запросами в
  минуту на админа.
- Создание: `POST …/campaigns` с заголовком `Idempotency-Key`; повтор того же запроса → та же
  кампания (`200`, `created:false`), тот же ключ с другим содержимым → `409 CONFLICT`.
  Отправка не чаще `RATE_PUSH_CAMPAIGNS_PER_HOUR` на админа.
- Разворачивание (fan-out) делает диспетчер: `queued → processing` (атомарный claim), затем
  `INSERT OR IGNORE … SELECT` по аудитории. Итоговый статус: `completed`, `partially_failed`,
  `failed`; отмена — `cancelled` (ещё не отправленные доставки отменяются).

### 3.4 Диспетчер, повторы и недействительные токены

| Ответ провайдера | Результат |
|---|---|
| принято | `provider_accepted`, `provider_message_id` |
| FCM `UNAVAILABLE`, `INTERNAL`, `QUOTA_EXCEEDED`, `UNAUTHENTICATED`, `THIRD_PARTY_AUTH_ERROR`, 429, 5xx; APNs `TooManyRequests`, `InternalServerError`, `ServiceUnavailable`, `Shutdown`, `ExpiredProviderToken`, `IdleTimeout`; сетевые ошибки и таймауты | `retrying` с задержкой |
| FCM `UNREGISTERED`, `SENDER_ID_MISMATCH`, `INVALID_ARGUMENT` по токену; APNs `BadDeviceToken`, `DeviceTokenNotForTopic`, `Unregistered`, `ExpiredToken`, 410 | `invalid_token`, установке ставится `push_status=invalid` (только если хэш токена не изменился) |
| прочие 4xx | `provider_failed` без повторов |

- Задержки: 5 с, 30 с, 2 мин, 10 мин, 30 мин (±20 % jitter); `Retry-After` провайдера
  учитывается, максимум 1 час; не больше `PUSH_MAX_ATTEMPTS` (по умолчанию 5) попыток.
- APNs: если окружение токена неизвестно и production ответил `BadDeviceToken`, один раз
  пробуется sandbox (и наоборот).
- Перед отправкой проверяется, что установка всё ещё принадлежит тому же пользователю, что и
  при постановке в очередь, иначе `skipped/recipient_changed` — так push прежнего аккаунта не
  попадёт к новому владельцу телефона.
- Прочие причины `skipped`: `token_inactive`, `disabled_in_app`, `permission_denied`,
  `provider_unavailable`, `token_unreadable`, `installation_missing`, `notification_missing`,
  `max_attempts`.

### 3.5 Идемпотентность и параллельность

Дубликат одного логического уведомления исключают ограничения БД, а не мьютексы:

| Риск | Защита |
|---|---|
| бизнес-событие пришло дважды, повтор транзакции | `notifications.dedupe_key UNIQUE` |
| двойной клик админа, повтор запроса после таймаута | `notification_campaigns.idempotency_key` (уникальный индекс) + сравнение содержимого |
| повторный fan-out, перезапуск воркера | `UNIQUE (notification_id, installation_id)` + `INSERT OR IGNORE` |
| несколько инстансов/воркеров | атомарный claim `UPDATE … WHERE id IN (SELECT … LIMIT) RETURNING` с арендой; результат пишется только при совпадении `lease_owner` и статусе `sending` |
| воркер упал во время отправки | аренда истекает через 5 минут, доставка берётся снова |

SQLite работает с одним писателем (`BEGIN IMMEDIATE`, WAL), поэтому claim сериализуется.
Тест `TestConcurrentWorkersNeverSendTheSameDeliveryTwice` запускает два сервиса на одном
файле БД и 120 устройств.

**Неустранимый случай «как минимум один раз»:** если провайдер принял сообщение, а ответ
потерялся (таймаут) или процесс упал до записи результата, доставка будет отправлена повторно.
Последствия гасятся на устройстве: Android заменяет уведомление с тем же `tag` (= id
уведомления), APNs — с тем же `apns-collapse-id`.

### 3.6 Метаданные запросов и журналы

- Заголовки клиента: `X-Platform`, `X-App-Version`, `X-App-Build`, `X-OS-Version`,
  `X-Installation-ID`, `X-Session-ID`, `X-Request-ID`, `traceparent`. Только метаданные:
  права по ним не решаются, недопустимые значения отбрасываются. `X-Request-ID`
  (`^[A-Za-z0-9._:-]{8,64}$`, иначе `req_…`) возвращается в заголовке ответа и в `error.request_id`.
- Структурный журнал (JSON, `slog`): `service`, `environment`, `request_id`, `route`
  (шаблон маршрута, не путь с id), `status`, `duration_ms`, `user_id`, `platform`, версии,
  короткий id установки, `trace_id`, `error_code`. Доставки: `event=push_delivery`,
  `delivery_id`, `notification_id`, `campaign_id`, `provider`, `attempt`, `status`,
  `error_code`, отпечаток токена, `duration_ms`.
- Четыре отдельных потока: структурный журнал (stdout), журнал входа `auth_events`
  (синхронно, сбой не ломает вход), аналитика приложений `app_events`/`app_sessions`
  (очередь в памяти, пакетная запись, при переполнении отбрасывается), аудит админов
  `admin_audit_logs` (неизменяемый через API, с `request_id`). Ошибки API приложений
  (≥ 400, кроме 401/404/405, без админ-панели и симулятора) пишутся в `api_errors`.
- Метрик Prometheus в проекте нет, новых систем не добавлено: показатели — в журнале и на
  дашборде админки (`/ops`).

### 3.7 Маскирование и PII

- Централизованно: `internal/redact` (ключи `password`, `otp`, `code` в формах входа,
  `authorization`, `cookie`, `access_token`, `refresh_token`, `id_token`, `*_private_key`,
  `*_secret`, `*_token`, `*_credentials`, …), фильтр в `logging` и в аудите.
- В журналы не попадают: пароли, OTP, токены доступа/обновления, Google/Apple id-token,
  push-токены (только отпечаток), ключи провайдеров, тексты сообщений.
- Почта и телефон хранятся только в `users`; события ссылаются на `user_id`. В журнале входа —
  `subject_hash` (HMAC почты/телефона), поиск по почте в админке находит и неудачные попытки.
- IP в админке показывается усечённым (`203.0.113.x`); полный IP доверяется
  `X-Forwarded-For` только при `TRUST_PROXY=true`.

### 3.8 Сроки хранения

| Данные | Переменная | По умолчанию |
|---|---|---|
| `app_events`, `app_sessions` | `RETENTION_APP_EVENTS_DAYS` | 90 дней |
| `api_errors` | `RETENTION_API_ERRORS_DAYS` | 30 дней |
| `auth_events` | `RETENTION_AUTH_EVENTS_DAYS` | 365 дней |
| завершённые доставки и личные уведомления | `RETENTION_NOTIFICATIONS_DAYS` | 180 дней |
| `admin_audit_logs` | `RETENTION_AUDIT_LOG_DAYS` | 0 = не удалять |

Очистка — фоновая, небольшими пачками (не блокирует писателя SQLite). Итоговые цифры
завершённой кампании сохраняются в `final_stats` и переживают удаление доставок.

## 4. Схема БД (миграции 0006–0008)

- `0006_push_notifications.sql`: `app_installations` (установка, необязательная ссылка на
  пользователя, метаданные, зашифрованный токен, разрешение, статус push),
  `notification_preferences`, расширение `notification_campaigns` (название, категория,
  ссылка, data, фильтр, `idempotency_key`, счётчики, время статусов), `notifications`
  (`dedupe_key UNIQUE`), `notification_deliveries` (outbox, аренда, статусы,
  `UNIQUE(notification_id, installation_id)`).
- `0007_telemetry.sql`: `app_sessions`, `app_events` (уникальность
  `(installation_id, client_event_id)` для повторов пакета), `auth_events`, `api_errors`,
  `admin_audit_logs.request_id` и индексы аудита.
- `0008_delivery_indexes.sql`: покрывающие индексы для статистики кампании, страницы доставок
  и сводки `/ops`.

Все миграции только добавляют (старые данные не трогаются), выполняются в транзакции при
старте (`DB_MIGRATE_ON_START=true`). Старая таблица `devices` и `POST /api/v1/devices`
оставлены для уже выпущенных сборок.

## 5. Android

- Firebase Messaging подключается всегда, плагин Google Services — только если есть
  `app/google-services.json`. Без файла сборка и приложение работают как раньше, push недоступен.
- Установка: UUID в `noBackupFilesDir` (не попадает в резервную копию; переустановка или
  восстановление = новая установка). Регистрация и события — только после принятия условий и
  только если сервер объявил `features.installations` / `features.telemetry`.
- Разрешение (Android 13+ `POST_NOTIFICATIONS`): не спрашивается при первом запуске; после входа
  на Home — мягкая карточка, в Настройках — раздел «Уведомления» (состояние системы, внутренний
  переключатель, категории, «Делиться диагностикой»).
- Каналы `general` (обычная важность) и `important` (высокая), названия локализованы, настройки
  пользователя не перезаписываются.
- `onNewToken` → регистрация; foreground-сообщения показываются через тот же канал и `tag`;
  нажатие обрабатывается в `onCreate` и `onNewIntent` (`singleTask`).
- Выход: `logout` с `X-Installation-ID`, затем анонимная регистрация.

## 6. iOS

- `aps-environment` только в Release (`Config/AIReply.entitlements`), Debug подписывается
  бесплатной Personal Team без push (как Sign in with Apple). Флаг `AIREPLY_PUSH_NOTIFICATIONS`
  (Debug `NO`, Release `YES`) → `AIReplyPushNotifications` в Info.plist.
- `@UIApplicationDelegateAdaptor`: делегат `UNUserNotificationCenter` назначается в
  `didFinishLaunching` (нажатие на уведомление при холодном старте не теряется), токен APNs →
  hex → регистрация с окружением из `embedded.mobileprovision` (development → sandbox; без
  профиля, т.е. App Store/TestFlight, → production).
- Установка: UUID в Keychain (`AfterFirstUnlockThisDeviceOnly`, только приложение). iOS
  сохраняет его при переустановке на том же iPhone и не переносит на другое устройство; токен
  APNs хранится вместе с id установки.
- Клавиатура (extension) отправляет только неидентифицирующие заголовки и никаких событий.

## 7. Админ-панель

Разделы: **Уведомления** (кампании, создание с предпросмотром и подтверждением, доставки,
устройства), **Журналы** (события приложений, входы, ошибки API, отчёт по версиям),
**Пользователь → Диагностика** (устройства, сессии, входы, ошибки, история уведомлений —
просмотр пишется в аудит), блок операций на **Дашборде**, фильтры и `request_id` в **Аудите**.

| Право | admin | viewer |
|---|---|---|
| `dashboard.read`, `users.read`, `settings.read`, `notifications.read` | ✓ | ✓ |
| `users.write`, `plans.write`, `settings.write`, `notifications.send` | ✓ | — |
| `users.diagnostics.read`, `logs.read`, `audit_logs.read` | ✓ | — |

Права проверяет сервер (`403 FORBIDDEN` с названием права); скрытые кнопки — только удобство.
Роли без `users.diagnostics.read` ищут пользователей только по точной почте, точному телефону
или началу id, видят более строгую маску и не могут фильтровать устройства/доставки по
пользователю. Сырые токены, хэши токенов, OTP, пароли и полные IP админка не получает никогда.

## 8. Настройка Firebase (Android)

1. Firebase Console → создать проект (или выбрать существующий).
2. Добавить Android-приложение с package name **`kz.yerek.aireply`** (для Google Sign-In
   добавьте SHA-1/SHA-256 ключей подписи debug/release).
3. Скачать `google-services.json` и положить в `AI-Reply-Android/app/` на машине сборки
   (файл в `.gitignore`; в CI — из секрета).
4. Project settings → Cloud Messaging: убедиться, что **Firebase Cloud Messaging API (V1)**
   включён (legacy server key не нужен и не используется).
5. Project settings → Service accounts → **Generate new private key** → JSON. Из него в `.env`
   сервера: `FIREBASE_PROJECT_ID` (`project_id`), `FIREBASE_CLIENT_EMAIL` (`client_email`),
   `FIREBASE_PRIVATE_KEY` (`private_key`, одной строкой с `\n` в кавычках или base64). Сам JSON
   никуда не коммитить и в приложение не класть.
6. `PUSH_NOTIFICATIONS_ENABLED=true`, перезапустить сервер; в админке на вкладке
   «Уведомления» FCM должен стать «настроен».

## 9. Настройка Apple Developer (iOS)

1. Certificates, Identifiers & Profiles → Identifiers → App ID **`kz.yerek.replykeyboard`** →
   включить **Push Notifications** (сертификаты не нужны — используется ключ .p8).
2. Keys → «+» → **Apple Push Notifications service (APNs)** → скачать `AuthKey_<KEYID>.p8`
   (скачивается один раз; хранить как секрет, в репозиторий не класть).
3. В `.env` сервера: `APNS_KEY_ID` (Key ID), `APNS_TEAM_ID` (Team ID, Membership),
   `APNS_PRIVATE_KEY` (содержимое .p8 одной строкой с `\n` или base64),
   `APNS_BUNDLE_ID=kz.yerek.replykeyboard`, `APNS_ENVIRONMENT=production`.
4. Сборка Release подписывается командой с платным аккаунтом: профиль должен содержать
   Push Notifications (Xcode с automatic signing обновит его сам). Debug на Personal Team
   продолжает работать без push.
5. Окружения: сборки из Xcode и Ad Hoc с development-профилем получают **sandbox**-токены,
   TestFlight и App Store — **production**. Приложение само сообщает окружение токена;
   `APNS_ENVIRONMENT` используется только если окружение неизвестно.

## 10. Переменные окружения

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `PUSH_NOTIFICATIONS_ENABLED` | `false` | включить отправку (установки регистрируются всегда) |
| `PUSH_WORKER_ENABLED` | `true` | запускать диспетчер в этом процессе |
| `PUSH_MAX_ATTEMPTS` | `5` | максимум попыток доставки (1–10) |
| `PUSH_BATCH_SIZE` | `50` | размер пачки claim |
| `PUSH_WORKER_CONCURRENCY` | `8` | параллельных отправок в пачке |
| `RATE_PUSH_CAMPAIGNS_PER_HOUR` | `10` | отправок кампаний на админа в час |
| `PUSH_LINK_HOSTS` | хост `PUBLIC_BASE_URL` | разрешённые хосты https-ссылок |
| `FIREBASE_PROJECT_ID`, `FIREBASE_CLIENT_EMAIL`, `FIREBASE_PRIVATE_KEY` | — | FCM HTTP v1 (все три вместе) |
| `APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_PRIVATE_KEY` | — | APNs token auth (все три вместе) |
| `APNS_BUNDLE_ID` | — | apns-topic, обязателен при APNs |
| `APNS_ENVIRONMENT` | `production` | `production` или `development` |
| `TELEMETRY_ENABLED` | `true` | приём событий приложений |
| `RATE_EVENTS_PER_MINUTE` | `30` | `/events` на установку в минуту (на IP ×20) |
| `RETENTION_*_DAYS` | см. §3.8 | сроки хранения |
| `TRUST_PROXY` | `false` | за reverse proxy — `true` (иначе все клиенты = один IP) |

Сервер не падает без ключей: без ключей вовсе он пишет предупреждение и не отправляет.
Наполовину заданный провайдер (например, `APNS_KEY_ID` без `APNS_PRIVATE_KEY`) — ошибка
конфигурации при старте с понятным текстом; нечитаемый ключ останавливает старт только при
`PUSH_NOTIFICATIONS_ENABLED=true`. `docker-compose.yml` передаёт весь `.env` (`env_file`),
отдельной пробрасывающей настройки не нужно; секреты в образ и compose-файл не попадают.

## 11. Локальная разработка и проверка

- Бэкенд: `cd ai-reply-back-end && go test ./...` (e2e-тесты с поддельными провайдерами —
  `internal/apptest/push_test.go`, `telemetry_test.go`, `review_fixes_test.go`; реальные push
  в тестах не отправляются). Локально push можно оставить выключенным.
- Android: `./gradlew assembleDebug testDebugUnitTest lintDebug`. В debug-сборке
  Настройки ▸ Developer ▸ «Simulate a push notification» проходит настоящий foreground-путь.
- iOS: `xcodebuild … test`. Симулятор: `-AIReplyDebugProvisionalPush YES` и
  `xcrun simctl push <device> kz.yerek.replykeyboard payload.json`, пример:

```json
{ "aps": { "alert": { "title": "Подписка", "body": "Осталось 3 дня" }, "sound": "default",
           "thread-id": "subscription" },
  "nid": "test-1", "did": "test-1", "type": "subscription_expiring",
  "category": "subscription", "link": "aireply://subscription" }
```

- Ручная проверка на устройствах после появления ключей: свежая установка (нет запроса при
  первом запуске) → вход → карточка → разрешить → кампания на себя (фильтр `user_ids`) →
  уведомление в foreground/background/после закрытия → нажатие открывает нужный экран →
  в админке у доставки есть «Открыто» → выход → push аккаунта больше не приходит → вход другим
  аккаунтом.

## 12. Продакшен

1. Задать ключи FCM и APNs и `PUSH_NOTIFICATIONS_ENABLED=true` в `.env` сервера.
2. Проверить `TRUST_PROXY=true`, если сервер за Caddy/nginx (лимиты и IP в журналах).
3. Развернуть бэкенд: миграции 0006–0008 применятся при старте.
4. Выпустить приложения с этой веткой (Android с `google-services.json`, iOS Release).
5. В админке: «Уведомления» → провайдеры настроены; тестовая кампания на свой `user_id`.
6. Несколько инстансов безопасны (claim + аренда); при желании воркер можно оставить на одном
   (`PUSH_WORKER_ENABLED=false` на остальных).

## 13. Диагностика проблем

| Симптом | Что проверить |
|---|---|
| `PUSH_DISABLED` при отправке | `PUSH_NOTIFICATIONS_ENABLED`, все три ключа провайдера |
| FCM `THIRD_PARTY_AUTH_ERROR` / `UNAUTHENTICATED` | ключ сервис-аккаунта, включён ли FCM API (V1) |
| FCM `SENDER_ID_MISMATCH` | `google-services.json` от другого проекта Firebase |
| APNs `BadDeviceToken` | окружение токена (sandbox vs production), `APNS_ENVIRONMENT` |
| APNs `DeviceTokenNotForTopic` | `APNS_BUNDLE_ID` ≠ bundle id приложения |
| APNs `InvalidProviderToken` | `APNS_KEY_ID`/`APNS_TEAM_ID`/ключ не совпадают |
| Нет получателей в предпросмотре | разрешение `denied`, переключатель выключен, категория выключена, установка старше 270 дней, провайдер не настроен |
| Устройство `replaced` | токен переехал на более новую установку (переустановка) — это нормально |
| Пользователи Android 142 не регистрируют токен | Журналы → События: `name=push_token_registration_failed`, `platform=android`, `app_build=142` (код ошибки, модель, версия ОС, `request_id`) |
| Ошибка в приложении с `request_id` | Журналы → Ошибки API → поиск по `request_id`; в журнале сервера та же строка |

## 14. Приватность: что собирается и зачем

| Данные | Зачем | Где | Кто видит | Срок |
|---|---|---|---|---|
| id установки (случайный UUID) | доставка push, анонимные кампании, ограничение частоты | `app_installations`, события | админ: первые 8 символов | пока установка активна |
| модель, производитель, ОС, версия и сборка приложения, язык, часовой пояс | совместимость, отчёт по версиям, локализация push | `app_installations`, события | админы | пока установка активна / 90 дней |
| push-токен | доставка | `app_installations` (зашифрован) | никто (только отпечаток) | до замены/недействительности |
| разрешение и переключатель уведомлений | не слать тем, кто отказался | `app_installations` | админы | пока установка активна |
| события приложения (список выше) | стабильность и проблемы push | `app_events`, `app_sessions` | `logs.read` | 90 дней |
| попытки входа (без почты и кодов, с HMAC) | безопасность | `auth_events` | `logs.read` | 365 дней |
| ошибки API (метаданные, без тел) | поддержка, качество версий | `api_errors` | `logs.read` | 30 дней |
| история уведомлений | статистика, поддержка | `notifications`, `notification_deliveries` | `notifications.read`; по человеку — `users.diagnostics.read` | 180 дней |
| действия админов | аудит | `admin_audit_logs` | `audit_logs.read` | бессрочно (настраивается) |

Не собирается никогда: тексты сообщений и ответов, нажатия клавиш, буфер обмена, IMEI/MAC/
серийные номера/рекламные id, точная геолокация. Клавиатура событий не отправляет. До принятия
условий приложения ничего не регистрируют и не отправляют. Пользователь может выключить
«Делиться диагностикой» (события перестают отправляться) и отдельные категории push.

### Правки для политики конфиденциальности (текст политики не менялся)

1. Добавить раздел о push-уведомлениях: зачем (аккаунт, подписка, безопасность, системные и
   рекламные сообщения), что нужно (разрешение ОС, push-токен Apple/Google), как отключить
   (настройки ОС, переключатель и категории в приложении; «безопасность» отключить нельзя).
2. Указать обработчиков: Google Firebase Cloud Messaging (Android) и Apple Push Notification
   service (iOS) — получают токен и содержимое уведомления для доставки.
3. Описать идентификатор установки: случайный, создаётся приложением, не связан с оборудованием
   и рекламой; на Android сбрасывается при переустановке, на iOS сохраняется в Keychain при
   переустановке на том же устройстве.
4. Перечислить технические данные устройства и диагностические события (п. 14), их цели и
   сроки хранения (90/30/365/180 дней), и что события связаны с аккаунтом, если пользователь
   вошёл; упомянуть переключатель «Делиться диагностикой».
5. Журнал безопасности входов: факт попытки, способ, результат, усечённый IP, без кодов и
   паролей, 365 дней.
6. Уточнить существующую формулировку «не собираем версию ОС» (если она есть): версия ОС и
   модель передаются в заголовках запросов и при регистрации установки.
7. Права пользователя: при удалении записи пользователя из БД его события и журнал входов
   удаляются каскадно, установки отвязываются. Самой функции удаления аккаунта в продукте
   сейчас нет (поле `deleted_at` нигде не выставляется) — это отдельная задача; App Store
   требует удаление аккаунта внутри приложения для приложений с регистрацией.
