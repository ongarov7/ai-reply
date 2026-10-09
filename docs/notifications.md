# Push-уведомления и письма уведомлений

Как устроены уведомления AI Reply: от регистрации установки до нажатия на push,
настройка Firebase (Android и iOS), переменные окружения, правила доставки и
локальная проверка. Контракт API — [ai-reply-back-end/docs/API.md](../ai-reply-back-end/docs/API.md#установки-и-push-уведомления),
клиенты — разделы «Push notifications» в [AI-Reply/README.md](../AI-Reply/README.md)
и [AI-Reply-Android/README.md](../AI-Reply-Android/README.md).

## 1. Архитектура

```
 бизнес-события (оплата, тариф, квота)        админ-панель: кампании (Idempotency-Key)
        │ NotifyUser(ключ события)                    │ аудитория считается в SQL
        ▼                                             ▼
 ┌──────────────────────── AI Reply backend (Go, SQLite) ─────────────────────────┐
 │ notifications.Service: язык получателя, тексты, notifications (UNIQUE ключ)    │
 │        ▼                                                                       │
 │ notification_deliveries — один outbox: push (строка на устройство) и письмо   │
 │        ▼  захват с арендой (lease) → диспетчер → повторы с backoff             │
 │   push ──► FCM HTTP v1 (сервисный аккаунт) ──► Android                         │
 │                                 └──► APNs (ключ .p8 в Firebase) ──► iOS        │
 │   письмо ──► Resend (адрес читается в момент отправки, только подтверждённый)  │
 │ installations.Service: app_installations, FCM-токены зашифрованы               │
 └────────────────────────────────────────────────────────────────────────────────┘
        ▲ POST /api/v1/installations · /notifications/opened · logout + X-Installation-ID
   приложения iOS и Android (только основное приложение, не клавиатура)
```

- Один провайдер — FCM — для обеих платформ. Прямой отправки в APNs нет: iOS-приложение
  отдаёт APNs-токен Firebase SDK и регистрирует на сервере FCM-токен.
- Ключи есть только у сервера (`.env`). В приложениях — клиентская конфигурация Firebase
  (не секрет, в git не кладётся).
- Бизнес-код не знает о FCM и Resend: он вызывает `NotifyUser`, отправляет только диспетчер.

Пакеты: `internal/installations`, `internal/push` (FCM без SDK), `internal/notifications`
(сервис, валидация, диспетчер, `events.go` — автоматические уведомления), `internal/email`
(Resend и шаблоны писем), `internal/repository` (`installations.go`, `notifications.go`,
`audience.go`, `subscription_notices.go`), `internal/transport/adminapi/notifications.go`.
Схема — миграция `0011_push_notifications.sql` (только добавления).

## 2. Путь уведомления

1. **Установка.** После принятия условий приложение создаёт UUID установки и вызывает
   `POST /api/v1/installations` (платформа, версия, модель, язык, разрешение, внутренний
   переключатель, FCM-токен). С access-токеном установка привязывается к аккаунту, без —
   анонимная. Аккаунт берётся только из токена.
2. **Событие.** Сервер создаёт одну строку `notifications` (уникальный `dedupe_key`) и
   доставки: по строке на каждое подходящее устройство и, если нужно, одну строку письма.
   Нет устройств или push выключен — одна строка `skipped` с причиной.
3. **Диспетчер** (горутина в каждом процессе) атомарно захватывает пачку доставок
   (`UPDATE … RETURNING`, аренда 5 минут), перед отправкой проверяет, что установка всё ещё
   принадлежит тому же аккаунту, и пишет результат, только пока аренда его.
4. **FCM** отвечает «принято», «повторить», «токен мёртв» или «отклонено». «Принято
   провайдером» — не «доставлено» и не «прочитано».
5. **Нажатие.** Приложение открывает `aireply://<экран>` (после согласия, входа и
   онбординга) и сообщает `POST /api/v1/notifications/opened` — сервер ставит `opened_at`,
   только если доставка ушла на эту установку.
6. **Выход.** `POST /api/v1/auth/logout` с `X-Installation-ID` отвязывает телефон (даже с
   истёкшим refresh-токеном), затем приложение регистрирует установку анонимно.
   Отключение пользователя и «отозвать сессии» в админке тоже отвязывают его устройства.

## 3. Язык

Текст пишется на сервере на языке получателя: `users.preferred_language` (выбор в
приложении, `PATCH /me`) → язык последнего устройства аккаунта (`locale` установки) →
`users.locale` → `en`. Для кампании язык решается для каждого устройства, текст берётся на
этом языке или на `fallback_locale`. Шаблоны автоматических уведомлений — ключи `push.<type>.*`
и `email.<type>.*` в `internal/localization/locales/{kk,ru,en,uz}.json` (тест требует
одинаковые ключи и плейсхолдеры во всех четырёх файлах).

## 4. Автоматические уведомления

| Тип | Когда | Ключ (одно уведомление на ключ) | Канал |
|---|---|---|---|
| `subscription_activated` | подтверждена оплата платного тарифа / админ выдал платный тариф | `subscription_activated:payment:<payment_id>` / `…:sub:<subscription_id>` | push + письмо |
| `subscription_expiring` | срок платного тарифа истекает в ближайшие 72 ч | `subscription_expiring:<sub>:<expires_ms>` | push |
| `subscription_expired` | истёк за последние 24 ч, другого платного нет | `subscription_expired:<sub>:<expires_ms>` | push |
| `quota_low` | после успешного ответа `0 < остаток ≤ max(1, ⌈лимит × PUSH_QUOTA_LOW_PERCENT / 100⌉)` | `quota_low:day:<YYYY-MM-DD>` / `quota_low:month:<YYYY-MM>` | push |
| `quota_exhausted` | остаток стал 0 или запрос упёрся в лимит | `quota_exhausted:day:<дата>` / `…:month:<месяц>` | push |

- Категория — `subscription`, ссылка — `aireply://subscription`.
- Никогда: бесплатный тариф, выдача при регистрации (`system`), аккаунт симулятора,
  старые install-токены (`legacy_install`), отключённые аккаунты.
- Оплата и выдача тарифа зовут события через интерфейсы `payments.Events` и `admin.Events`;
  сбой уведомления только пишется в журнал и не откатывает оплату. Повторное подтверждение
  того же платежа нового уведомления не создаёт.
- Напоминания о сроке — в 15-минутной задаче `expireSubscriptions`; уже отправленные
  отфильтровываются в SQL, продление даёт новый срок и новое напоминание.
- Квота считается после атомарного резерва (`ReserveQuota` возвращает новые счётчики), без
  лишних запросов: арифметика → «уже отправлено» в памяти → одна запись в БД с таймаутом
  2 с. Порог для лимитов 7 / 30 / 50 — 1 / 3 / 5. Месячные — только при
  `monthly_message_limit > 0`. Дата — в `DEFAULT_TIMEZONE`. Сброс квоты админом в тот же
  день уведомления повторно не вызывает.

### Письмо «тариф подключён»

Рендерится в момент отправки из ключей `email.subscription_activated.{subject, greeting,
body, expires, manage, footer}` (тот же макет, что у письма с кодом входа: HTML + текст).
`{plan}` — название тарифа на языке письма, `{limit}` — дневной лимит, `{date}` — дата
окончания (строка `expires` пропадает у бессрочного тарифа). Адрес берётся из аккаунта в
момент отправки и только подтверждённый (`email_verified_at`), иначе `skipped
no_verified_email`. `Idempotency-Key` в Resend — `notification-<delivery_id>`. Письмо
подтверждает покупку, поэтому уходит и при выключенной категории `subscription` (выключается
только push).

## 5. Кампании из админки

Админ-панель ▸ Уведомления: создание (тексты kk/ru/en, uz по желанию, язык по умолчанию
`ru`), предпросмотр аудитории по языкам, отправка с подтверждением, статистика, журнал
доставок и реестр устройств.

- Аудитория (все условия через И, только активные обычные аккаунты): сегмент
  (`free` | `paid` | `demo` — тариф с демо-оплаты или пробный), тарифы, подписка
  (`active` | `expired`), платформы, языки, квота сегодня (`has_remaining` |
  `near_exhaustion` | `exhausted`), конкретные люди по id или почте (≤ 500).
- `Idempotency-Key` обязателен: двойной клик и повтор после таймаута возвращают ту же кампанию,
  тот же ключ с другим содержимым — `409`. Отправок не больше `RATE_PUSH_CAMPAIGNS_PER_HOUR`
  в час на админа; повтор уже отправленной кампании лимит не тратит. Аудит: `campaign.create`
  (аудитория кратко, без адресов и id людей), `campaign.send`, `campaign.cancel`.
- Разворачивает кампанию диспетчер: одна строка `notifications` на язык
  (`campaign:<id>:<язык>`), доставки — `INSERT OR IGNORE … SELECT` по аудитории.
  Итог: `completed`, `partially_failed` или `failed`; отмена останавливает неотправленное, а
  отправка, которая шла в момент отмены, не повторяется (`cancelled campaign_cancelled`).
- Ключи `data` FCM запрещает: `from`, `message_type` и всё, что начинается с `google` или
  `gcm`, — такие (и наши служебные `nid`, `did`, `type`, …) форма отклоняет.

## 6. Доставка, повторы, идемпотентность

| Ответ FCM | Результат |
|---|---|
| принято | `provider_accepted` |
| `UNAVAILABLE`, `INTERNAL`, `QUOTA_EXCEEDED`, `UNAUTHENTICATED`, 429, 5xx, сеть, таймаут | `retrying`: 5 с, 30 с, 2 мин, 10 мин, 30 мин (±20 %), `Retry-After` учитывается (≤ 1 ч), до `PUSH_MAX_ATTEMPTS` |
| `UNREGISTERED`, `SENDER_ID_MISMATCH`, `INVALID_ARGUMENT` по токену | `invalid_token`; у установки `push_status = invalid` (если хэш токена не сменился). Тот же токен при перерегистрации остаётся `invalid` — приложение берёт новый; после `SENDER_ID_MISMATCH` (может быть ошибкой проекта Firebase на сервере) тот же токен снова `active` |
| `THIRD_PARTY_AUTH_ERROR` (нет APNs-ключа в Firebase), прочие 4xx | `provider_failed` без повторов |

Письма: 5xx, 429 и сеть — повтор по тому же расписанию, прочие 4xx — `provider_failed`.
Ошибка чтения из БД при отправке (`notification_lookup_failed`, `installation_lookup_failed`)
— тоже повтор. Автоматические уведомления захватываются раньше строк кампании: большая
кампания не задерживает письмо и push о покупке. В журнале доставок `push` — отпечаток
токена, на который ушла именно эта попытка.
Причины `skipped`: `push_disabled`, `no_devices`, `disabled_by_user`, `recipient_changed`
(на телефоне уже другой аккаунт), `token_inactive`, `disabled_in_app`, `permission_denied`,
`email_disabled`, `no_verified_email`, `email_template_missing`.

| Риск | Защита |
|---|---|
| событие пришло дважды, два процесса | `notifications.dedupe_key UNIQUE` |
| двойной клик админа | уникальный `notification_campaigns.idempotency_key` + сравнение содержимого |
| повторный fan-out | уникальные индексы доставок + `INSERT OR IGNORE` |
| несколько воркеров | захват `UPDATE … RETURNING` с арендой, запись результата только владельцем аренды |
| воркер упал во время отправки | аренда истекает, доставка берётся снова (если кампанию за это время отменили — `cancelled`) |
| остановка сервера (SIGTERM) | новые отправки не начинаются: начатые дописывают результат, захваченные, но не начатые возвращаются в очередь |

Остаётся случай «как минимум один раз»: FCM принял, а ответ потерялся — доставка уйдёт
повторно. На устройстве дубль заменяется: Android — тот же `tag`, iOS — тот же
`apns-collapse-id` (оба = id уведомления).

Сроки хранения: завершённые доставки старше `RETENTION_NOTIFICATIONS_DAYS` удаляются
пачками; ожидающие — никогда. Личное уведомление без доставок удаляется не раньше чем через
40 дней, даже при меньшем сроке: его ключ не даёт повторить «раз в месяц». Итог кампании,
в том числе отменённой, остаётся в `final_stats`.

## 7. Настройка Firebase

Пошаговая инструкция с точными путями файлов: `docs/FIREBASE_SETUP.md`. Ниже кратко.

Один проект Firebase на оба приложения.

1. **Проект.** Использовать существующий `ai-reply-4bf8f`. Project settings ▸ Cloud Messaging:
   включён **Firebase Cloud Messaging API (V1)** (legacy-ключ не нужен).
2. **Android.** Приложение `kz.yerek.aireply` уже зарегистрировано; настоящий
   `google-services.json` находится в `AI-Reply-Android/app/` и включён в Git.
3. **iOS.** Приложение `kz.ai-reply.reply.keyboard.keyboard` уже зарегистрировано;
   настоящий `GoogleService-Info.plist` включён в Git в `AI-Reply/Config/Firebase/` (шаг сборки «Firebase config»
   копирует его в приложение; в Xcode добавлять не нужно) или значения в build settings
   `FIREBASE_*` (см. `AI-Reply/README.md`).
4. **APNs-ключ.** Apple Developer ▸ Identifiers ▸ App ID `kz.ai-reply.reply.keyboard.keyboard`
   → включить *Push Notifications*. Keys ▸ «+» ▸ *Apple Push Notifications service (APNs)* →
   скачать `AuthKey_<KEY_ID>.p8` (скачивается один раз, хранить как секрет). Firebase ▸
   Project settings ▸ Cloud Messaging ▸ *Apple app configuration* ▸ *APNs Authentication Key*
   ▸ Upload: файл `.p8`, Key ID, Team ID. Для новых ключей выбрать нужное окружение Sandbox / Production и минимальную область
   Topic Specific для bundle ID приложения; загрузить каждый в соответствующий слот Firebase.
5. **Сервер.** Отдельный аккаунт `ai-reply-fcm-sender` с ролью
   `roles/firebasecloudmessaging.admin` уже создан; его JSON хранить отдельно от Git.
   В `.env` сервера — либо путь `FIREBASE_SERVICE_ACCOUNT_FILE`, либо три значения из файла.
   В Docker файл монтируется только для чтения (строка-образец в `docker-compose.yml`)
   или задаются три переменные.
   JSON не коммитить и не класть в приложения.
6. `PUSH_NOTIFICATIONS_ENABLED=true`, перезапуск. Админка ▸ Уведомления: FCM «настроен».

## 8. Переменные окружения

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `PUSH_NOTIFICATIONS_ENABLED` | `false` | отправка push (установки регистрируются всегда) |
| `FIREBASE_SERVICE_ACCOUNT_FILE` | — | путь к JSON сервисного аккаунта |
| `FIREBASE_PROJECT_ID`, `FIREBASE_CLIENT_EMAIL`, `FIREBASE_PRIVATE_KEY` | — | то же тремя значениями; ключ — PEM одной строкой с `\n` или base64 |
| `PUSH_WORKER_ENABLED` | `true` | диспетчер в этом процессе |
| `PUSH_MAX_ATTEMPTS` | `5` | попыток доставки (1–10) |
| `PUSH_BATCH_SIZE` / `PUSH_WORKER_CONCURRENCY` | `50` / `8` | размер захвата / параллельных отправок |
| `RATE_PUSH_CAMPAIGNS_PER_HOUR` | `10` | отправок кампаний на админа в час |
| `PUSH_LINK_HOSTS` | хост `PUBLIC_BASE_URL` | хосты `https://`-ссылок (и их поддомены); приложения открывают только `ai-reply.kz` и его поддомены |
| `PUSH_QUOTA_LOW_PERCENT` | `10` | порог `quota_low` (1–100) |
| `RETENTION_NOTIFICATIONS_DAYS` | `180` | хранение завершённых доставок (0 — не удалять); личные уведомления — не меньше 40 дней |
| `NOTIFICATION_EMAILS_ENABLED` | `true` | письма уведомлений (работают, только если настроен Resend) |

Без ключей сервер стартует: предупреждение в журнале, `features.push_notifications = false`,
события пишутся как `skipped push_disabled`. Задан только один-два из трёх `FIREBASE_*`,
или и файл, и переменные — ошибка конфигурации при старте. Нечитаемый ключ останавливает
старт при `PUSH_NOTIFICATIONS_ENABLED=true`; сам ключ в журнал и текст ошибки не попадает.

## 9. Локальная проверка

- **Тесты:** `cd ai-reply-back-end && go test ./...` (поддельные FCM и почта, настоящая
  SQLite): `internal/apptest/push_test.go`, `notification_admin_test.go`,
  `notification_user_test.go`, `notification_events_test.go`.
- **Без Firebase:** `make run`, войти в приложение (демо-код `1111`), купить тариф в
  демо-режиме или сделать ответы до порога → журнал доставок в админке («Уведомления»):
  строки `skipped / push_disabled`, письмо — если настроен Resend.
- **С Firebase:** заполнить `.env`, собрать приложения с конфигурацией Firebase, разрешить
  уведомления, затем кампания на себя (аудитория — своя почта) или оплата в демо-режиме.
  Проверить foreground, background, после закрытия приложения; нажатие открывает экран, в
  журнале доставок появляется «открыто»; после выхода push этого аккаунта не приходят.
- **Только приложение iOS (Simulator, Debug с `-AIReplyDebugProvisionalPush YES`):**
  `xcrun simctl push <device> kz.ai-reply.reply.keyboard.keyboard payload.json`:

```json
{ "aps": { "alert": { "title": "Подписка скоро закончится", "body": "Тариф «Pro» действует до 12.03.2026." },
           "sound": "default", "thread-id": "subscription" },
  "nid": "test-1", "did": "", "type": "subscription_expiring",
  "category": "subscription", "link": "aireply://subscription" }
```

## 10. Выкладка и диагностика

1. Перед деплоем: `SELECT name FROM schema_migrations` на сервере. Если там есть
   `0006_push_notifications.sql` (когда-то запускалась старая ветка), миграция 0011 упадёт
   на уже существующих таблицах — разобрать вручную до деплоя.
2. Настроить Firebase и `.env`, выкатить бэкенд (0011 применится при старте), проверить
   «Уведомления» в админке, затем выпускать приложения.

| Симптом | Что проверить |
|---|---|
| `409 PUSH_DISABLED` при отправке кампании | `PUSH_NOTIFICATIONS_ENABLED`, ключи Firebase |
| iOS: `provider_failed THIRD_PARTY_AUTH_ERROR` | APNs-ключ не загружен в Firebase, неверный Key ID / Team ID, push не включён для App ID |
| `invalid_token SENDER_ID_MISMATCH` | конфигурация приложения из другого проекта Firebase |
| `invalid_token UNREGISTERED` | приложение удалено или токен устарел — приложение пришлёт новый при запуске |
| `token_unreadable` | сменили `JWT_ACCESS_SECRET` (им шифруются токены) — приложения перерегистрируются сами |
| в предпросмотре 0 получателей | разрешение `denied`, выключен переключатель или категория, установка старше 270 дней, push выключен; для категории «Новости» (marketing) — только аккаунты, которые сами её включили (opt-in, по умолчанию выключена) |
| письмо `skipped no_verified_email` | у аккаунта нет подтверждённой почты (вход по телефону); Apple private relay требует домен отправителя, зарегистрированный в Apple |

Push-токены хранятся зашифрованными, в журналах и админке — только отпечаток
`fcm:1a2b3c4d`; параметры шаблонов (`params`) не уходят на устройство; текст сообщений
пользователей уведомления не содержат.
