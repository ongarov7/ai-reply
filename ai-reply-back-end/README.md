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
# заполнить OPENAI_API_KEY и три секрета: make secrets
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
open http://localhost:8080/simulator # интерактивный симулятор продукта
```

Демо-вход в приложении (только разработка): «Продолжить с e-mail», любой адрес
и код **1111** — если `AUTH_DEMO_MODE=true` и Resend не настроен. С настроенным
Resend код всегда случайный и приходит письмом. В production демо-режим не
запускается, а без `RESEND_API_KEY` / `RESEND_FROM_EMAIL` сервер откажется стартовать.

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
  users/             профиль и устройства
  plans/             каталог тарифов
  subscriptions/     подписки и расчёт текущего лимита (entitlement)
  ai/                промпт, клиент OpenAI Responses API, шлюз с учётом токенов
  productevents/     события онбординга и настроек из приложений: закрытый список, только счётчики
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
  что и админка (`docs/SIMULATOR.md`);
- полная совместимость со старым контрактом (`/v1/auth/register`, `/v1/reply/generate`),
  поэтому уже собранные iOS/Android-версии продолжают работать.

## Приватность (проверяется тестами)

| Что | Ответ |
|---|---|
| Ключ OpenAI в мобильном коде | НЕТ — только `.env` на сервере |
| Приложение ходит в OpenAI напрямую | НЕТ — только через бэкенд |
| Текст сообщения в БД | НЕТ — в схеме нет такой колонки |
| Текст виден администратору | НЕТ — в admin API нет таких полей |
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
  `APPLE_CLIENT_ID`);
- реальный эквайринг — один интерфейс `payments.Provider`;
- push: проект Firebase с приложениями `kz.yerek.aireply` и `kz.ai-reply.reply.keyboard.keyboard`,
  APNs-ключ в Firebase, сервисный аккаунт и `PUSH_NOTIFICATIONS_ENABLED=true` в `.env` —
  [../docs/notifications.md](../docs/notifications.md);
- rate limiter в памяти → Redis при нескольких инстансах;
- `APP_ENV=production`, HTTPS, `ADMIN_SECURE_COOKIES=true`, `AUTH_DEMO_MODE=false`.

Подробности: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md),
[docs/API.md](docs/API.md), [docs/AUTH.md](docs/AUTH.md), [docs/AI_QUALITY.md](docs/AI_QUALITY.md),
[docs/MOBILE_MIGRATION.md](docs/MOBILE_MIGRATION.md).
