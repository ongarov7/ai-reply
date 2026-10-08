# AI Reply — Backend

Go-бэкенд для AI Reply: REST API для iOS/Android, AI-шлюз (ключ провайдера живёт
только здесь), тарифы и квоты, админ-панель на Vue и публичный лендинг.
Языки интерфейса: **қазақша, русский, English, oʻzbekcha**.

```
Mobile (iOS / Android / клавиатура)
        │  Bearer access token
        ▼
   AI Reply Backend  ──►  Auth ──► Quota ──► AI Gateway ──► OpenAI
        │                                   (ключ только на сервере)
        ├── SQLite (только метаданные: тексты переписки не хранятся)
        ├── outbox уведомлений ──► FCM (Android и iOS) · Resend (письма)
        ├── /admin  — Vue SPA: дашборд, пользователи, тарифы, аудит, уведомления
        └── /       — лендинг (kk / ru / en / uz)
```

## Быстрый старт

```bash
cp .env.example .env
# заполнить OPENAI_API_KEY, ADMIN_PASSWORD и три секрета (make secrets):
# с заглушками REPLACE… / CHANGE_ME сервер не стартует
make run           # http://localhost:8080 (порт = APP_PORT из .env)
```

Или в Docker:

```bash
cp .env.example .env
make docker        # = docker compose up -d --build
```

### Порт и деплой за Caddy

Единственный источник порта — `APP_PORT` в `.env`. Из него берутся:

- адрес, который слушает Go-сервер (`APP_HOST:APP_PORT`; в контейнере `APP_HOST=0.0.0.0`);
- проброс порта Docker — только на localhost: `127.0.0.1:APP_PORT → контейнер:APP_PORT`;
- healthcheck контейнера — `http://127.0.0.1:$APP_PORT/healthz`.

В Dockerfile, docker-compose.yml и коде порта нет; без `APP_PORT` сервер и
`docker compose` не стартуют. Сменить порт: поправить `APP_PORT` в `.env` и
пересоздать контейнер (`docker compose up -d`), затем обновить upstream в Caddy:

```
reverse_proxy 127.0.0.1:{APP_PORT}
```

База SQLite лежит в томе `aireply-data` (`/app/data/aireply.db`) и переживает
`docker compose down` / `up -d`. Не запускайте `docker compose down -v` — это удалит базу.

При старте автоматически применяются миграции, создаются тарифы
(`free 7/день`, `standard 30/день`, `pro 50/день`) и администратор из
`ADMIN_EMAIL` / `ADMIN_PASSWORD`.

Проверка:

```bash
curl localhost:8080/healthz
curl localhost:8080/api/v1/plans
open http://localhost:8080/          # лендинг
open http://localhost:8080/admin     # админка
open http://localhost:8080/simulator # симулятор продукта (только при SIMULATOR_ENABLED)
```

Демо-вход в приложении (только разработка): «Продолжить с e-mail», любой адрес
и код **1111** — если `AUTH_DEMO_MODE=true` и Resend не настроен. С настроенным
Resend код всегда случайный и приходит письмом. По умолчанию `AUTH_DEMO_MODE=false`;
`true` сервер принимает только при `APP_ENV=development` или `test` (staging и
production не стартуют), а без `RESEND_API_KEY` / `RESEND_FROM_EMAIL` production
откажется стартовать. Вход по телефону без демо-режима отвечает
`AUTH_PROVIDER_UNAVAILABLE`: SMS-провайдера нет, и сервер не делает вид, что код ушёл.

Безопасные значения по умолчанию для релиза: `LEGACY_API_ENABLED=false` (старый
`/v1/*` API для install-token сборок; согласия в нём нет, поэтому при `APP_ENV=production`
значение `true` не даёт серверу стартовать), `PAYMENT_MODE=off` (`off | demo | live`;
production — только `off` или `live`), `PAYMENT_DEMO_CHECKOUT=true` допустим только
вместе с `PAYMENT_MODE=demo` и только при `APP_ENV=development` или `test`. Для
разработки их включают явно в `.env`.

Публичные страницы для магазинов: `/privacy`, `/offer`, `/support` (контакт —
`CONTACT_EMAIL`, реальный ящик; в production обязателен, иначе сервер не стартует;
вне production без него адрес нигде не показывается) и
`/account/delete` (`/delete-account` → редирект): удаление аккаунта в приложении
или по коду на почту. Оператор в юридических текстах — `LEGAL_OPERATOR_NAME` и
`LEGAL_OPERATOR_DETAILS`: без них тексты называют только AI Reply и контакт, а production
при старте пишет предупреждение — заполнить до подачи в магазины. Про Sign in with Apple
страница удаления и политика говорят «отключим» только при заданных `APPLE_TEAM_ID`,
`APPLE_KEY_ID`, `APPLE_PRIVATE_KEY`; без них — только «можно убрать в настройках Apple ID».

## Структура

```
cmd/server/          точка входа: конфиг → БД → миграции → сервисы → HTTP
config/              загрузка и валидация окружения (без секретов в коде)
migrations/          SQL-схема, встроена в бинарник
internal/
  traits/            общие мелочи: UUID, часы, обрезка текста, маскирование
  phone/             E.164 для 12 стран (KZ, RU, UZ, KG, TJ, TM, AZ, GE, UA, BY, TR, AE)
  domain/            сущности и доменные ошибки
  database/          SQLite: WAL, один писатель, пул читателей, миграции
  repository/        SQL-слой (в т.ч. атомарный учёт квоты)
  auth/              OTP, JWT (15 мин), refresh с ротацией, PBKDF2 для админов
    appleid/         отзыв токена Sign in with Apple при удалении аккаунта (ES256 client_secret)
  users/             профиль, согласие, удаление аккаунта
  plans/             каталог тарифов
  subscriptions/     подписки и расчёт текущего лимита (entitlement)
  ai/                промпт, клиент OpenAI Responses API, шлюз с учётом токенов
  productevents/     события онбординга и настроек из приложений: закрытый список, только счётчики
  reports/           жалобы на AI-ответы (приложения → админка «Жалобы»)
  retention/         удаление просроченных кодов, сессий, установок и метаданных (RETENTION_*)
  payments/          интерфейс эквайринга + demo-адаптер
  installations/     реестр установок приложений, зашифрованные push-токены
  push/              FCM HTTP v1 без SDK (Android и iOS), классификация ошибок
  notifications/     уведомления, кампании, outbox-диспетчер (push и письма), бизнес-события
  email/             Resend: код входа и письма уведомлений
  localization/      kk/ru/en/uz для веба и админки
  middleware/        request-id, логи, паника, CORS, заголовки, rate limit
  simulator/       демо-аккаунт, демо-данные и демо-слой тарифов для /simulator
  transport/
    httpx/           конверт ошибок, разбор JSON, IP клиента
    api/             /api/v1/* для мобильных + совместимость со старым /v1/*
    adminapi/        /api/v1/admin/* для SPA
    simulatorapi/    /api/v1/simulator/* для интерактивной демонстрации
    web/             лендинг (SSR + Vue-острова), оболочка админки и симулятора
  apptest/           сквозные тесты всего стека
```

## Что уже работает

- вход: iOS — Apple / Google / почта, Android — Google / почта. Код на почту —
  4 цифры, через Resend, в БД только HMAC, 5 минут, 5 попыток, повтор через 32 с;
  Google и Apple ID-токены проверяются на сервере по JWKS (подпись, `iss`, `aud`,
  `exp`, `sub`, `nonce`). Подробно — [docs/AUTH.md](docs/AUTH.md);
- access (15 мин) + refresh с ротацией и детектом переиспользования;
- профиль, устройства, тарифы, подписки, usage;
- AI-ответ через сервер: квота → провайдер → учёт токенов и стоимости;
- качество ответа: версионированные промпты (`reply_v2`, `compose_v2`), язык
  ответа решает входящее сообщение (казахское — ответ на казахском, даже при русских
  телефоне, раскладке и инструкции), род отправителя для русских форм, очистка
  вывода, проверка и одна исправляющая попытка, подсказка `/ai/polish` без квоты,
  набор оценки (`internal/ai/eval`) — [docs/AI_QUALITY.md](docs/AI_QUALITY.md);
- лимиты живут в БД: 30 → 50 в день меняется в админке без релиза приложения;
- события онбординга и настроек (`POST /api/v1/analytics/events`): закрытый список
  имён и значений, счётчики по именам в дашборде админки;
- админка: дашборд с графиками, пользователи, CRUD тарифов, аудит, настройки;
- push-уведомления через FCM на Android и iOS — см. раздел ниже;
- лендинг на 4 языках с анимированной демонстрацией работы клавиатуры;
- вход по телефону из приложений убран; эндпоинты `/auth/request-otp` и
  `/auth/verify-otp` оставлены только для уже установленных сборок;
- `/simulator` — интерактивный симулятор продукта для показа клиентам:
  iOS и Android, AI-клавиатура, голосовой ввод, архитектура, админ-демо;
  генерация идёт через настоящий бэкенд, вход — теми же логином и паролем,
  что и админка (`docs/SIMULATOR.md`). Включён только при `SIMULATOR_ENABLED=true`
  (по умолчанию — в development/test); ссылок на него на публичных страницах нет;
- старый контракт `/v1/auth/register`, `/v1/reply/generate` — только при
  `LEGACY_API_ENABLED=true` (по умолчанию выключен; согласие на нём не проверяется,
  принимается только install-token, в production сервер с ним не стартует).

## Приватность (проверяется тестами)

| Что | Ответ |
|---|---|
| Ключ OpenAI в мобильном коде | НЕТ — только `.env` на сервере |
| Приложение ходит в OpenAI напрямую | НЕТ — только через бэкенд |
| Текст сообщения в БД | НЕТ — в схеме нет такой колонки |
| Текст виден администратору | НЕТ — кроме жалоб (`/api/v1/admin/reports`): причина, комментарий пользователя и сгенерированный ответ, если пользователь оставил «Отправить текст ответа вместе с жалобой»; скопированное сообщение и инструкция туда не попадают |
| Текст в логах | НЕТ — логируются только метаданные |
| Текст подсказки `/ai/polish` (заметка и исправленный вариант) в БД или логах | НЕТ — только длина, токены и `mode = 'polish'` |
| Род отправителя в логах, событиях или admin JSON | НЕТ — хранится только в профиле и идёт в промпт |
| Свободный текст в продуктовых событиях | НЕТ — только имена, коды, числа 0…1000 и bool из закрытого списка сервера |
| Push-токен в логах / админке | НЕТ — в БД зашифрован, наружу только отпечаток `fcm:1a2b3c4d` |
| Ключ Firebase в приложениях | НЕТ — сервисный аккаунт только в `.env` сервера |

Тест `TestMessageContentIsNeverPersisted` отправляет уникальную строку через
`/api/v1/ai/reply` и ищет её в файле БД, WAL и логах — любой найденный след
роняет сборку. `TestComposeContentIsNeverPersisted` и `TestPolishTextIsNeverPersisted`
делают то же для «Создать» и для подсказки polish, `TestProductEventsNeverStoreText`
— для продуктовых событий.

## Push-уведомления

Одна очередь (`notification_deliveries`) для push и писем; отправляет диспетчер
в фоне, повторяет временные ошибки, выключает мёртвые токены. FCM доставляет и на
Android, и на iOS (через APNs-ключ, загруженный в Firebase). Без ключей сервер
работает: события пишутся как `skipped`, `features.push_notifications = false`.

- Приложения регистрируют установку (`POST /api/v1/installations`, FCM-токен,
  язык, разрешение); выход с `X-Installation-ID` отвязывает телефон от аккаунта.
- Автоматически: тариф подключён (оплата или админ — push и письмо), тариф скоро
  закончится / закончился, ответы на сегодня почти закончились / закончились.
  Каждое событие — не больше одного уведомления (уникальный ключ в БД).
- Кампании из админки: тексты на kk/ru/en (uz по желанию), каждому — на его языке,
  аудитория считается на сервере, `Idempotency-Key`, статистика по языкам.
- Настройка: `PUSH_NOTIFICATIONS_ENABLED=true` и сервисный аккаунт Firebase
  (`FIREBASE_SERVICE_ACCOUNT_FILE` или `FIREBASE_PROJECT_ID` / `_CLIENT_EMAIL` /
  `_PRIVATE_KEY`); письма — уже настроенный Resend.

Подробно: [../docs/notifications.md](../docs/notifications.md), API — [docs/API.md](docs/API.md#установки-и-push-уведомления).

## Команды

```bash
make test        # все тесты
make test-race   # включая гонки на квоте
make vet
make build
make secrets     # сгенерировать JWT/legacy секреты
```

## Что осталось до продакшна

- Resend: подтвердить домен `ai-reply.kz` и прописать `RESEND_*` в `.env` сервера
  (без них `APP_ENV=production` не стартует) — [docs/AUTH.md](docs/AUTH.md);
- Google / Apple: client ID в `.env` (`GOOGLE_CLIENT_ID_IOS`, `GOOGLE_CLIENT_ID_WEB`,
  `APPLE_CLIENT_ID=kz.ai-reply.reply.keyboard.keyboard` — именно bundle id приложения);
  для отзыва токена Sign in with Apple при удалении аккаунта (App Store 5.1.1(v)) —
  ключ «Sign in with Apple» из Apple Developer: `APPLE_TEAM_ID`, `APPLE_KEY_ID`,
  `APPLE_PRIVATE_KEY` (PEM `.p8`, одной строкой с `\n` или base64). Без ключа сервер
  стартует с предупреждением, а тексты не обещают отключение Apple;
- `CONTACT_EMAIL` — реальный ящик поддержки (в production обязателен);
  `LEGAL_OPERATOR_NAME` и `LEGAL_OPERATOR_DETAILS` (наименование и реквизиты оператора) —
  обязательно до подачи в магазины: без них production стартует с предупреждением;
- реальный эквайринг — один интерфейс `payments.Provider`;
- push: проект Firebase с приложениями `kz.yerek.aireply` и `kz.ai-reply.reply.keyboard.keyboard`,
  APNs-ключ в Firebase, сервисный аккаунт и `PUSH_NOTIFICATIONS_ENABLED=true` в `.env` —
  [../docs/notifications.md](../docs/notifications.md);
- rate limiter в памяти → Redis при нескольких инстансах.

### Чек-лист деплоя

- сначала проверить `.env` новым бинарником, не трогая работающий контейнер:
  `docker compose build backend && docker compose run --rm --no-deps backend -check`
  (или `go run ./cmd/server -env /path/.env -check`): список всего, что не даст стартовать,
  и предупреждения; база и порт не открываются. Что поменялось — раздел ниже;
- `APP_ENV=production`, `AUTH_DEMO_MODE=false`, `PAYMENT_MODE=off`, `LEGACY_API_ENABLED=false`
  (`true` в production не стартует), `SIMULATOR_ENABLED=false` (по умолчанию вне development/test);
- `PUBLIC_BASE_URL=https://ai-reply.kz`: cookie админки тогда `Secure` и идёт HSTS
  (`ADMIN_SECURE_COOKIES` можно задать явно — явное значение главнее);
- `TRUST_PROXY=true` за Caddy: IP клиента — последний адрес в `X-Forwarded-For`
  (тот, что добавил прокси), `X-Real-IP` не читается;
- секреты из `make secrets`, `ADMIN_PASSWORD` не короче 16 символов; значения-заглушки
  (`REPLACE…`, `CHANGE_ME`) сервер не принимает. Смена `ADMIN_PASSWORD` и рестарт —
  это ротация: новый хэш, все сессии админа закрываются, запись в аудите;
- `CONTACT_EMAIL` — реальный ящик на `ai-reply.kz` (и Reply-To всех писем); без него
  production не стартует. `LEGAL_OPERATOR_NAME` / `LEGAL_OPERATOR_DETAILS` — до подачи
  в магазины (иначе предупреждение при старте);
- `REVIEW_LOGIN_EMAIL` + `REVIEW_LOGIN_CODE` — только на время проверки App Review /
  Google Play: новый код на каждую подачу, после одобрения обе переменные очистить
  ([docs/AUTH.md](docs/AUTH.md#вход-для-проверки-app-review--google-play));
- `APPLE_CLIENT_ID=kz.ai-reply.reply.keyboard.keyboard`; `APPLE_TEAM_ID`, `APPLE_KEY_ID`,
  `APPLE_PRIVATE_KEY` — отзыв Sign in with Apple при удалении аккаунта (без них —
  предупреждение при старте). При старте в журнале строка `sign-in client ids` со
  списками Google и Apple; каждый отклонённый токен — одно предупреждение
  `identity token rejected` с причиной (`audience_mismatch` + `token_aud` и
  `configured_aud`, `nonce_mismatch` + `nonce_form`, `expired`, `bad_signature`,
  `unknown_key_id`, `issuer_mismatch`, `missing_email`, `missing_nonce`, `malformed`),
  без токена и почты; ответ клиенту прежний (`401 INVALID_ID_TOKEN`);
- `APP_STORE_URL` / `PLAY_STORE_URL` — когда приложения опубликованы (лендинг покажет ссылки);
- бэкап тома `aireply-data` по расписанию и вне сервера: `sqlite3 aireply.db ".backup aireply-$(date +%F).db"`
  или копия тома при остановленном контейнере (файлы `-wal`/`-shm` вместе с базой);
- порядок релиза: сначала сборки с экраном согласия (iOS начиная с `ea80a49`, Android —
  с `e28bf9d`) в TestFlight / internal track, потом бэкенд. После смены версий документов
  на `2026-10-08` старые сборки получают `403 CONSENT_REQUIRED` и показывают в клавиатуре
  «войдите снова»; тестерам — закрыть приложение и открыть заново (экран согласия
  появляется при холодном запуске), а не выходить из аккаунта ([docs/API.md](docs/API.md));
- откат: не откатывать production на сборку до `a1c7e38` (миграция `0012`, в том числе на
  `fb39678`): она принимает только `PAYMENT_MODE=demo|live`, в production — только `live`,
  а там `live` всё ещё подключён к демо-провайдеру, который одобряет любую оплату, и
  checkout игнорирует видимость тарифов — платные тарифы становятся бесплатными. Если
  без отката никак: `PAYMENT_MODE=live`, закрыть `POST /api/v1/payments/*` в Caddy на всё
  время отката, после возврата проверить `payments` с `provider='demo' AND status='succeeded'`
  и созданные из них подписки (`source='payment'`). Откат на `e56b511` совместим: схема и `.env` те же.

Хранение данных: фоновая задача удаляет просроченное пачками — коды входа
(`RETENTION_OTP_DAYS=30`), завершённые сессии (`RETENTION_REFRESH_TOKENS_DAYS=30`
после истечения или отзыва), установки без аккаунта (`RETENTION_ANON_INSTALLATIONS_DAYS=180`
с последнего появления), события приложения и метаданные запросов
(`RETENTION_PRODUCT_EVENTS_DAYS` / `RETENTION_AI_USAGE_EVENTS_DAYS`, по 400), завершённые
доставки уведомлений (`RETENTION_NOTIFICATIONS_DAYS=180`; сами уведомления — не меньше 40 дней).
`0` — не удалять. Эти же числа выводит политика конфиденциальности. Не удаляются
автоматически: журнал действий администраторов (id аккаунта без почты, и после удаления
аккаунта — это сказано в политике) и кампании уведомлений; при удалении аккаунта его id и
почты убираются из получателей кампаний (остаётся только счётчик `redacted_people`), а
итоговая статистика завершённой кампании не меняется, когда её доставки удаляет retention.

### Обновление существующего production `.env`

Что изменилось после `fb39678` (прошлый релиз) и что сделать с уже работающим `.env`.
Проверка: `docker compose run --rm --no-deps backend -check` (после `docker compose build backend`)
или `go run ./cmd/server -env /path/.env -check` — печатает все проблемы сразу, ничего не открывает.

Не даст стартовать (исправить до `docker compose up -d --build`, иначе контейнер уйдёт в
перезапуски). Новое или ужесточённое:

- `APP_ENV` — только `development | test | staging | production` (регистр и пробелы не важны);
  `prod`, `live` и т. п. — отказ;
- `LEGACY_API_ENABLED=true` при `APP_ENV=production` — отказ. Раньше по умолчанию было `true`,
  и прежний `.env.example` содержал `true`: поставить `false` или удалить строку;
- `CONTACT_EMAIL` — обязателен в production и должен быть простым адресом (`support@ai-reply.kz`);
- `ADMIN_PASSWORD` — в production не короче 16 символов (было 10), если задан `ADMIN_EMAIL`;
- заглушки `REPLACE…` / `CHANGE_ME` в `OPENAI_API_KEY`, `JWT_ACCESS_SECRET`, `JWT_REFRESH_SECRET`,
  `RESEND_API_KEY`, `ADMIN_PASSWORD` (при `ADMIN_EMAIL`), `AUTH_SIGNING_SECRET` (при
  `LEGACY_API_ENABLED=true`);
- `AUTH_SIGNING_SECRET` равен одному из JWT-секретов (при `LEGACY_API_ENABLED=true`);
- `AUTH_DEMO_MODE=true` — только в development/test (раньше запрещён лишь в production;
  по умолчанию было `true`, теперь `false`);
- `PAYMENT_MODE` — `off | demo | live` (было `demo | live`, по умолчанию `demo`; теперь `off`);
- `PAYMENT_DEMO_CHECKOUT=true` — только вместе с `PAYMENT_MODE=demo` и только в development/test;
- `RESEND_FROM_EMAIL`, `CONTACT_EMAIL`, `REVIEW_LOGIN_EMAIL` — простой адрес без имени;
  `REVIEW_LOGIN_EMAIL` и `REVIEW_LOGIN_CODE` — вместе, код из 4 цифр;
- `APPLE_TEAM_ID`, `APPLE_KEY_ID`, `APPLE_PRIVATE_KEY` — вместе и только с `APPLE_CLIENT_ID`;
  ключ, который не читается как ES256 `.p8`, — отказ при старте;
- `APP_STORE_URL` / `PLAY_STORE_URL` — только страницы `https://apps.apple.com/…` /
  `https://play.google.com/…`.

Не изменились, но тоже блокируют старт: `APP_PORT` (1–65535), `OPENAI_API_KEY`,
`JWT_ACCESS_SECRET` / `JWT_REFRESH_SECRET` (≥ 32 символов и разные), `AUTH_SIGNING_SECRET`
≥ 32 при `LEGACY_API_ENABLED=true`, `DEFAULT_TIMEZONE` (имя из базы часовых поясов),
`PAYMENT_MODE=demo` в production, `PUBLIC_BASE_URL` на `http://` в production,
`RESEND_API_KEY` + `RESEND_FROM_EMAIL` в production, `AUTH_DEMO_OTP` — 4 цифры при демо-режиме,
`ADMIN_PASSWORD` ≥ 10 вне production, Firebase — `FIREBASE_SERVICE_ACCOUNT_FILE` читается
либо три `FIREBASE_*` вместе (не оба способа сразу), `PUSH_MAX_ATTEMPTS` 1–10,
`PUSH_QUOTA_LOW_PERCENT` 1–100, ключ Firebase, который не читается, при
`PUSH_NOTIFICATIONS_ENABLED=true`.

Изменился смысл или значение по умолчанию (проверить вручную):

- `RATE_OTP_REQUEST_PER_HOUR` — теперь только на IP (по умолчанию 30; было 5 и на IP, и на
  адрес) и действует также на `/api/v1/account/delete/request`. Строку `=5` из прежнего
  `.env.example` удалить или поставить 30: за CGNAT оператора 5 запросов в час на один IP
  блокируют вход всем. Лимит на адрес — новая `RATE_OTP_REQUEST_PER_ADDRESS_PER_HOUR` (5);
- `RATE_OTP_VERIFY_PER_HOUR` — по умолчанию 60 (было 10), на IP, действует и на
  `/api/v1/account/delete/confirm`. Строку `=10` удалить или поставить 60;
- `TRUST_PROXY=true` — IP клиента теперь правый (добавленный прокси) адрес
  `X-Forwarded-For`, `X-Real-IP` не читается; за Caddy менять ничего не нужно;
- `ADMIN_SECURE_COOKIES` — пусто означает `true`, если `PUBLIC_BASE_URL` на https (раньше —
  только в production); явное `false` из прежнего `.env.example` на https-сервере удалить;
- `CONTACT_EMAIL` — запасного `hello@aireply.app` больше нет, адрес приводится к нижнему
  регистру; `RESEND_FROM_EMAIL` тоже;
- `LEGACY_API_ENABLED=true` (вне production) — `/v1/*` принимает только install-token,
  access-токен `/api/v1` там больше не подходит;
- `OTP_CHANNEL=stub` без `AUTH_DEMO_MODE` — вход по телефону отвечает `AUTH_PROVIDER_UNAVAILABLE`;
- `APPLE_CLIENT_ID` — должен быть bundle id `kz.ai-reply.reply.keyboard.keyboard`; значение
  не в форме bundle id — предупреждение при старте, чужой bundle id (например старый
  `kz.yerek.replykeyboard`) — `audience_mismatch` в журнале при каждой попытке входа;
- `RETENTION_NOTIFICATIONS_DAYS` — по умолчанию по-прежнему 180; строки уведомлений живут не
  меньше 40 дней, при значении меньше 40 политика показывает оба срока.

Новые, значения по умолчанию безопасны: `RATE_OTP_REQUEST_PER_ADDRESS_PER_HOUR=5`,
`OTP_MAX_FAILED_PER_DAY=10`, `REVIEW_LOGIN_EMAIL` / `REVIEW_LOGIN_CODE` (пусто),
`LEGAL_OPERATOR_NAME` / `LEGAL_OPERATOR_DETAILS` (пусто — предупреждение в production),
`APPLE_TEAM_ID` / `APPLE_KEY_ID` / `APPLE_PRIVATE_KEY` (пусто при Apple-входе — предупреждение),
`SIMULATOR_ENABLED` (пусто — только development/test), `APP_STORE_URL` / `PLAY_STORE_URL`,
`PAYMENT_DEMO_CHECKOUT=false`, `LIMIT_POLISH_PER_DAY=100`, `RATE_AI_REPORTS_PER_HOUR=20`,
`RETENTION_OTP_DAYS=30`, `RETENTION_REFRESH_TOKENS_DAYS=30`, `RETENTION_ANON_INSTALLATIONS_DAYS=180`,
`RETENTION_PRODUCT_EVENTS_DAYS=400`, `RETENTION_AI_USAGE_EVENTS_DAYS=400`.

Предупреждения при старте (`configuration warning: …`, сервер работает): пустой
`LEGAL_OPERATOR_NAME` в production; Apple-вход без ключа отзыва; `APPLE_CLIENT_ID` не похож
на bundle id. Лимит входа в админку теперь в коде: 5 попыток за 15 минут на пару адрес+IP и
20 неудач в час на адрес — после этого отказ получают только IP, которые сами ошибались с
этим адресом (`RATE_ADMIN_LOGIN_PER_HOUR` на IP — как раньше).

Подробности: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md),
[docs/API.md](docs/API.md), [docs/AUTH.md](docs/AUTH.md), [docs/AI_QUALITY.md](docs/AI_QUALITY.md),
[docs/MOBILE_MIGRATION.md](docs/MOBILE_MIGRATION.md).
