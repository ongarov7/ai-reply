# Вход в AI Reply: почта, Google, Apple

| Платформа | Способы входа |
|---|---|
| iOS | Continue with Apple · Continue with Google · Continue with Email |
| Android | Continue with Google · Continue with Email |

Вход по номеру телефона (WhatsApp / SMS) из обоих приложений удалён. Всё, что
не касается входа (ответы для WhatsApp и других мессенджеров, клавиатура),
не менялось. Старые эндпоинты `/api/v1/auth/request-otp` и `/verify-otp`
работают для уже установленных сборок.

Источник правды — сервер: он сам проверяет код из письма и ID-токены Google и
Apple. Приложения ничего не решают о подлинности, они только передают данные.

## 1. Данные

Миграция `0005_auth_providers.sql` — только `ALTER TABLE … ADD COLUMN`,
`UPDATE` для заполнения и один индекс. Ни одна таблица не пересоздаётся,
существующие пользователи и сессии не затрагиваются.

- `users` — аккаунт. Новое поле `email_verified_at`.
- `auth_identities` — способы входа аккаунта: `(kind, value)` с `UNIQUE`,
  где `kind` ∈ `email`, `google`, `apple`, `phone` (старые аккаунты), а
  `value` — нормализованный адрес или `sub` провайдера. Новые поля:
  `provider_email`, `provider_email_verified`, `updated_at`.
- `otp_codes` — коды. Новые поля: `purpose` (`login` / `link_email`),
  `consumed_reason` (`verified`, `superseded`, `expired`, `attempts`,
  `delivery_failed`), `updated_at`; индекс `idx_otp_recent` для лимитов.

## 2. Код на почту

| Правило | Как |
|---|---|
| 4 цифры | `crypto/rand`, `%04d` — ведущие нули сохраняются |
| Хранение | только `HMAC-SHA256`, ключ выводится из `JWT_ACCESS_SECRET` |
| Срок | 5 минут (`OTP_TTL`) |
| Попытки | 5 на код (`OTP_MAX_ATTEMPTS`), затем `OTP_ATTEMPTS_EXCEEDED` |
| Повторная отправка | не раньше 32 с (`OTP_RESEND_COOLDOWN`), проверяет сервер |
| Новый код | гасит предыдущий (`superseded`) |
| Одноразовость | погашение — условный `UPDATE`: из двух параллельных проверок проходит одна |
| Лимиты | IP и адрес: `RATE_OTP_REQUEST_PER_HOUR`; адрес: `RATE_OTP_REQUEST_PER_DAY`; проверка: `RATE_OTP_VERIFY_PER_HOUR` |
| Перечисление аккаунтов | ответ одинаков для нового и известного адреса |
| Логи | ни код, ни токены, ни ключ Resend не пишутся |

Нормализация адреса — одна функция `auth.NormalizeEmail` (trim, lower-case,
RFC 5322 без display name, ≤ 254 символов). Приложения повторяют её только для
кнопки «Продолжить».

Письмо — `internal/email`: интерфейс `email.Sender`, реализация Resend с
таймаутом 10 с, одним повтором на 5xx / сетевую ошибку, `Idempotency-Key`,
HTML и текстовой частью на языке пользователя (kk / ru / en / uz). Если Resend
отказал, код сразу гасится, клиент получает `EMAIL_DELIVERY_FAILED` и может
запросить новый без ожидания.

## 3. Google и Apple

Проверка (`internal/auth/idtoken`, только стандартная библиотека):

- подпись `RS256` по JWKS провайдера; ключи кэшируются по `Cache-Control`
  (5 мин – 24 ч), неизвестный `kid` перечитывает ключи не чаще раза в 30 с,
  при недоступности провайдера используются последние известные ключи;
- `iss`, `aud` (список client ID), `exp`, `iat`, `nbf` с допуском 60 с, `sub`;
- `nonce`: Google — точное совпадение; Apple — `SHA256(nonce)` в hex;
  сравнение за постоянное время.

Связывание аккаунтов (консервативно):

1. Уже известный `(provider, sub)` → тот же аккаунт, всегда.
2. Новый `sub`, и провайдер — источник истины для адреса (Google: `@gmail.com`
   или домен из claim `hd`; Apple: `@icloud.com`, `@me.com`, `@mac.com`,
   private relay) → привязка к аккаунту с этой почтой.
3. Новый `sub`, адрес занят, провайдер не источник истины → `EMAIL_ALREADY_IN_USE`:
   пользователь входит кодом на почту.
4. Иначе создаётся новый аккаунт; без подтверждённой провайдером почты — отказ.

Аккаунты «только телефон» получают в настройках пункт «Добавить почту для
входа» (`/api/v1/me/email/otp/*`), чтобы не потерять доступ после выхода.

## 4. Настройка Resend (ai-reply.kz)

1. resend.com → **Domains** → Add `ai-reply.kz`, добавить у регистратора DNS-записи,
   которые покажет Resend (DKIM `resend._domainkey`, MX/TXT для `send`), дождаться
   статуса **Verified**. Рекомендуется DMARC: `_dmarc TXT "v=DMARC1; p=none;"`.
2. **API Keys** → Create: permission **Sending access**, domain `ai-reply.kz`.
3. В `.env` **сервера** (не в репозиторий):
   ```
   RESEND_API_KEY=re_…
   RESEND_FROM_EMAIL=noreply@ai-reply.kz
   RESEND_FROM_NAME=AI Reply
   ```
   При `APP_ENV=production` без этих значений сервер не стартует.
4. Ключ, который когда-либо попадал в чат, почту или тикет, нужно перевыпустить
   (Resend → API Keys → Revoke / Create) и заменить в `.env`.

## 5. Настройка Google

Google Cloud Console → APIs & Services:

1. **OAuth consent screen**: название AI Reply, домен `ai-reply.kz`, ссылки на
   политику и оферту; scopes `openid`, `email`, `profile`. Опубликовать (Production).
2. **Credentials → Create OAuth client ID**:
   - **iOS**: bundle ID `kz.ai-reply.reply.keyboard.keyboard` → client ID в
     `GOOGLE_CLIENT_ID_IOS` (сервер) и в build setting `GOOGLE_IOS_CLIENT_ID`
     (Xcode, таргет AIReply). В `GOOGLE_IOS_REVERSED_CLIENT_ID` — тот же ID
     наоборот: `com.googleusercontent.apps.<id>`.
   - **Web application** → client ID в `GOOGLE_CLIENT_ID_WEB` (сервер) и в
     `aireply.googleWebClientId` (Android: `local.properties`,
     `~/.gradle/gradle.properties`, `-P…` или переменная `GOOGLE_WEB_CLIENT_ID`).
   - **Android**: пакет `kz.yerek.aireply` + SHA-1 каждого ключа подписи (debug,
     upload и ключ Google Play App Signing). В коде этот ID не нужен, но без него
     Google не выдаст токен приложению.
3. Пустые значения — кнопка Google скрыта в release-сборках, сервер отвечает
   `features.google_sign_in=false`.

## 6. Настройка Apple

1. developer.apple.com → Identifiers → `kz.ai-reply.reply.keyboard.keyboard` → включить
   **Sign in with Apple** (в проекте уже есть entitlement
   `com.apple.developer.applesignin`; Xcode с автоматической подписью обновит
   профиль сам).
2. `.env` сервера: `APPLE_CLIENT_ID=kz.ai-reply.reply.keyboard.keyboard`.
3. Скрытые адреса Apple (`@privaterelay.appleid.com`): Certificates, IDs &
   Profiles → Services → **Sign in with Apple for Email Communication** →
   добавить домен `ai-reply.kz` (и `send.ai-reply.kz` — домен обратного адреса
   Resend) и адрес `noreply@ai-reply.kz`; SPF и DKIM из шага 4 должны проходить.
   Без этого письма с кодом на такие адреса Apple не доставит (вход через Apple
   при этом работает).

## 7. Проверка после деплоя

```bash
curl -s https://ai-reply.kz/api/v1/config | jq .features
# {"reply_preferences":true,"email_otp":true,"google_sign_in":true,"apple_sign_in":true}

curl -s -X POST https://ai-reply.kz/api/v1/auth/email/otp/request \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","locale":"ru"}'
# {"masked_email":"y***u@example.com","expires_in":300,"resend_after":32,"code_length":4}
```

Письмо должно прийти от `AI Reply <noreply@ai-reply.kz>`; повторный запрос в
течение 32 с — `429 OTP_RESEND_COOLDOWN`.
