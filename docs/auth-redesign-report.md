# Отчёт: новая аутентификация AI Reply (30.09.2026)

## 1. Итог

- iOS: **Continue with Apple / Continue with Google / Continue with Email**.
- Android: **Continue with Google / Continue with Email**.
- Код на почту — 4 цифры через Resend; сервер — единственный источник правды:
  он проверяет код и ID-токены Google и Apple (JWKS, `iss`, `aud`, `exp`, `sub`, `nonce`).
- Вход, восстановление и подтверждение по телефону (WhatsApp/SMS) из обоих
  приложений удалены. Остальная работа с WhatsApp (ответы на сообщения, клавиатура) не тронута.
- Переключатель «Вибрация при нажатии» на обеих платформах, по умолчанию включён.
- Миграция только аддитивная; существующие аккаунты и сессии сохраняются.

## 2. Архитектура

```
iOS app ──(Apple SDK: identity token + SHA256(nonce))──┐
iOS app ──(GoogleSignIn SDK: ID token + nonce)──────────┤
Android ──(Credential Manager: ID token + nonce)────────┤──► /api/v1/auth/{apple,google}
iOS/Android ──(email)──► /auth/email/otp/request ──► Resend ──► письмо с кодом
iOS/Android ──(email+code)──► /auth/email/otp/verify ─┘
                                   │
            auth.Service ── idtoken (JWKS-кэш) ── email.Sender (Resend) ── repository (SQLite)
                                   │
                         users ◄── auth_identities (provider, subject) UNIQUE
```

Пакеты бэкенда: `internal/auth` (сервис, OTP, провайдеры, нормализация почты),
`internal/auth/idtoken` (проверка JWT по JWKS, только stdlib), `internal/email`
(интерфейс `Sender`, реализация Resend, шаблоны), `internal/repository`
(`otp.go`, `identities.go`), `internal/transport/api/signin_handlers.go`.

## 3. Модель данных и миграция

`migrations/0005_auth_providers.sql` — только `ADD COLUMN`, заполняющие `UPDATE`
и индекс `idx_otp_recent`:

- `users.email_verified_at`;
- `auth_identities.provider_email`, `provider_email_verified`, `updated_at`;
- `otp_codes.purpose`, `consumed_reason`, `updated_at`.

Уникальность `(kind, value)` в `auth_identities` уже была — она и есть
`UNIQUE(provider, provider_subject)`. Тест `TestAuthProvidersMigrationKeepsExistingAccounts`
проверяет, что после миграции прежние аккаунты входят как раньше.

## 4. Код на почту (OTP)

4 цифры из `crypto/rand` (ведущие нули сохраняются); в БД только `HMAC-SHA256`
с ключом, выведенным из `JWT_ACCESS_SECRET`; 5 минут; 5 попыток; новый код гасит
старый; повторная отправка не раньше 32 с — проверяет сервер; погашение атомарное
(из двух одновременных проверок проходит одна); лимиты на IP и на адрес (час и сутки);
ответ одинаков для нового и известного адреса. Нормализация почты — одна функция
`auth.NormalizeEmail`.

## 5. Отправка писем (Resend)

`email.Sender` → `email.Resend`: таймаут 10 с, один повтор на 5xx/сетевую ошибку,
`Idempotency-Key`, отправитель `AI Reply <noreply@ai-reply.kz>`, HTML + текстовая
часть на kk/ru/en/uz. Код, ключ и токены не пишутся в логи. Если Resend отказал,
код гасится и клиент получает `EMAIL_DELIVERY_FAILED` (можно сразу запросить новый).
В `APP_ENV=production` без `RESEND_API_KEY`/`RESEND_FROM_EMAIL` сервер не стартует.

## 6. Проверка токенов Google и Apple

RS256 по JWKS (кэш по `Cache-Control` 5 мин – 24 ч, перечитывание неизвестного `kid`
не чаще раза в 30 с, при сбое — последние известные ключи); `iss`, `aud`, `exp`,
`iat`, `nbf` (допуск 60 с), `sub`; `nonce`: Google — как есть, Apple — SHA-256 hex;
сравнение за постоянное время. Токены в логи не пишутся.

## 7. Связывание аккаунтов

Известный `(provider, sub)` → тот же аккаунт. Новый `sub` привязывается к аккаунту
с той же почтой только если провайдер — источник истины для адреса (Gmail / домен
`hd`; iCloud / private relay) и почта подтверждена. Иначе — `EMAIL_ALREADY_IN_USE`,
пользователь входит кодом на почту. Аккаунты «только телефон» добавляют почту в
Настройках («Добавить почту для входа»).

## 8. Эндпоинты

| Метод | Путь |
|---|---|
| POST | `/api/v1/auth/email/otp/request` |
| POST | `/api/v1/auth/email/otp/verify` |
| POST | `/api/v1/auth/google` |
| POST | `/api/v1/auth/apple` |
| POST | `/api/v1/me/email/otp/request` |
| POST | `/api/v1/me/email/otp/verify` |
| GET | `/api/v1/config` → `features.email_otp`, `google_sign_in`, `apple_sign_in` |

`/auth/request-otp`, `/auth/verify-otp` оставлены для установленных сборок.
`user.auth_providers` в ответах `/me` и сессии. Подробно: `ai-reply-back-end/docs/API.md`,
`docs/openapi.yaml`.

## 9. Коды ошибок

`INVALID_EMAIL` 400, `INVALID_OTP` 400 (+`attempts_remaining`), `OTP_EXPIRED` 400,
`OTP_ALREADY_USED` 400, `OTP_ATTEMPTS_EXCEEDED` 429, `OTP_RESEND_COOLDOWN` 429
(+`retry_after_seconds`, `Retry-After`), `EMAIL_DELIVERY_FAILED` 503,
`EMAIL_ALREADY_IN_USE` 409, `INVALID_ID_TOKEN` 401 (не истёкшая сессия — клиенты
не делают refresh), `AUTH_PROVIDER_UNAVAILABLE` 503. У каждой ошибки свой текст в приложениях.

## 10. Переменные окружения

| Переменная | Значение |
|---|---|
| `RESEND_API_KEY` | ключ Resend — **только в `.env` сервера** |
| `RESEND_FROM_EMAIL` | `noreply@ai-reply.kz` |
| `RESEND_FROM_NAME` | `AI Reply` |
| `GOOGLE_CLIENT_ID_IOS` | iOS OAuth client ID |
| `GOOGLE_CLIENT_ID_WEB` | Web OAuth client ID (его же использует Android) |
| `APPLE_CLIENT_ID` | `kz.yerek.replykeyboard` |
| `OTP_TTL` / `OTP_MAX_ATTEMPTS` | `5m` / `5` |
| `OTP_RESEND_COOLDOWN` | `32s` |
| `RATE_OTP_REQUEST_PER_HOUR` / `RATE_OTP_REQUEST_PER_DAY` | `5` / `10` |
| `AUTH_DEMO_OTP` | ровно 4 цифры (только разработка) |

Сборка: iOS — build settings `GOOGLE_IOS_CLIENT_ID`, `GOOGLE_IOS_REVERSED_CLIENT_ID`;
Android — `aireply.googleWebClientId` (`local.properties` / `-P` / `GOOGLE_WEB_CLIENT_ID`).

## 11. iOS

`SignInView` (кнопка Apple — системная `SignInWithAppleButton`, Google, e-mail),
`EmailSignInView`, `VerifyCodeView` (поле `.oneTimeCode`, автоотправка на 4-й цифре,
таймер повтора от сервера, «Изменить почту»), `LinkEmailView` в Настройках.
GoogleSignIn-iOS 9.2 через SPM, изолирован в `GoogleSignInProvider` (`internal import`);
без client ID и URL-схемы кнопка скрывается, SDK не может упасть. Entitlement
Sign in with Apple, проверка отзыва Apple ID при запуске, выход очищает сессии Apple/Google.
Сессия после входа отдаётся сразу, регистрация устройства — в фоне.

## 12. Android

`SignInScreen` (Google, e-mail), `EmailSignInScreen`, `VerifyCodeScreen`
(авто-отправка, таймер повтора, Back → почта), `LinkEmailDialog` в Настройках.
Google — Credential Manager (`GetSignInWithGoogleOption` + nonce), клиент
`GoogleSignInClient`; выход вызывает `clearCredentialState`. Регистрация устройства
после входа идёт в фоне приложения, а не в scope экрана. R8-правило для Credential Manager.

## 13. Вибрация клавиатуры

iOS: Настройки ▸ Клавиатура ▸ «Вибрация при нажатии», значение в App Group
(`shared.keyboardHaptics`), клавиатура читает его (и работает только с Full Access).
Android: тот же переключатель, `SettingsStore.keyboardHaptics`, `KeyFeedback`
его учитывает; системная настройка вибрации тоже действует. По умолчанию — включено.

## 14. Локализация

Все новые строки — en / kk / ru / uz на iOS (String Catalog), Android (`values*`),
в письме (бэкенд `locales/*.json`). Удалены строки телефонного входа.
Android Lint: 80 старых строк без узбекского перевода (не связаны со входом)
переведены в предупреждение — продуктовое правило «узбекский покрывает все основные
экраны, редкие старые тексты — английский» проверяет `LocalizationParityTest`.

## 15. Что удалено

Выбор «Телефон/Почта», поле номера, выбор страны, тексты про номер — на iOS и Android.
`CountryDto`/`Country`, старые `requestCode/verifyCode` в клиентах. Серверные
эндпоинты для старых сборок сохранены.

## 16. Файлы

Бэкенд, новые: `migrations/0005_auth_providers.sql`, `internal/auth/{email,emailotp,providers}.go`,
`internal/auth/idtoken/*`, `internal/email/*`, `internal/repository/{otp,identities}.go`,
`internal/transport/api/signin_handlers.go`, `docs/AUTH.md`, тесты
`internal/apptest/{email_auth,provider_auth,migration}_test.go`, `internal/auth/otp_test.go`.
Изменены: `config`, `cmd/server/main.go`, `internal/auth/{otp,service}.go`, `domain`,
`httpx`, `api/{server,dto,me_handlers,auth_handlers}.go`, `users/service.go`, `locales/*.json`,
`.env.example`, `README.md`, `docs/API.md`, `docs/openapi.yaml`.

iOS, новые: `Features/Account/{AuthForms,AppleSignIn,GoogleSignInProvider,EmailSignInView,LinkEmailView}.swift`,
`Tests/SignInTests.swift`, `Package.resolved`. Изменены: `AccountModel`, `SignInView`,
`VerifyCodeView`, `AccountGateView`, `AccountSettingsSection`, `AIReplyApp`,
`Shared/Account/*`, `SharedSettings`, `KeyboardViewController`, `Info.plist`,
`AIReply.entitlements`, `Localizable.xcstrings`, `project.pbxproj`, `project.yml`, `README.md`.

Android, новые: `data/account/{GoogleSignInClient,SignInInput}.kt`,
`ui/feature/account/{EmailSignInScreen,LinkEmailDialog}.kt`, тесты `SignInTest`,
`KeyboardHapticsSettingTest`. Изменены: `AccountController`, `SignInScreen`,
`VerifyCodeScreen`, `AccountSettings`, `AppNavHost`, `ServiceLocator`, `MainActivity`,
`data/account/*`, `SettingsStore`, `KeyFeedback`, `ReplyKeyboardService`, `SettingsScreen`,
строки `values*`, `build.gradle.kts`, `libs.versions.toml`, `proguard-rules.pro`, `README.md`.

Репозиторий: корневой `.gitignore`; из индекса убраны `.DS_Store`, `*.log`,
`ios-simlog.txt`, `android-shot*.png` (файлы на диске остались); `HANDOFF.md`.

## 17. Тесты и проверки

| Проверка | Результат |
|---|---|
| `go build`, `go vet ./...`, `gofmt` | чисто |
| `go test ./...` | 128 тестовых функций, все зелёные (63 новые) |
| `go test -race` (auth, idtoken, email, config, apptest-вход) | зелёные |
| iOS `xcodebuild test` (симулятор) | 153 теста, 0 падений (SignInTests — 16) |
| Android `assembleDebug testDebugUnitTest` | 128 тестов, 0 падений |
| Android `lintDebug` | 0 ошибок |
| Android `minifyReleaseWithR8` | успешно |
| Скан секретов (рабочая копия + вся история git) | ключей не найдено |

`docker compose build` здесь не запускался (нет Docker-демона); Dockerfile и
`go.mod` не менялись, та же сборка `go build` проходит.

## 18. Ручная настройка Resend, Google, Apple

Пошагово — `ai-reply-back-end/docs/AUTH.md` (разделы 4–6): домен `ai-reply.kz`
в Resend (DKIM/SPF), ключ с правом Sending access; OAuth-клиенты Google (iOS,
Web, Android с SHA-1 всех ключей подписи); Sign in with Apple для App ID и
регистрация домена для писем на private relay.

## 19. Деплой

```bash
git push origin main                     # пуш — вручную
# сервер, каталог ai-reply-back-end:
git pull
docker compose stop backend
# копия БД до миграции (том aireply-data; работает и с остановленным контейнером)
docker run --rm --volumes-from "$(docker compose ps -aq backend)" -v "$PWD":/backup alpine \
  tar czf /backup/aireply-data-$(date +%F).tgz -C /app data
# .env: RESEND_API_KEY, RESEND_FROM_EMAIL, RESEND_FROM_NAME,
#       GOOGLE_CLIENT_ID_IOS, GOOGLE_CLIENT_ID_WEB, APPLE_CLIENT_ID,
#       OTP_RESEND_COOLDOWN=32s, RATE_OTP_REQUEST_PER_DAY=10
docker compose up -d --build
docker compose logs --tail=50 backend    # строка старта: email_otp=email google_sign_in=… apple_sign_in=…
curl -s https://ai-reply.kz/api/v1/config | jq .features
```

Приложения: iOS — задать `GOOGLE_IOS_CLIENT_ID` / `GOOGLE_IOS_REVERSED_CLIENT_ID`,
Archive; Android — `./gradlew bundleRelease -Paireply.googleWebClientId=…`.
Сначала сервер, потом приложения.

## 20. Риски и следующие шаги

1. Ключ Resend был отправлен в чат — перевыпустить и заменить в `.env`.
2. Аккаунты «только телефон»: новые сборки не могут войти по номеру. Пользователь
   должен добавить почту в Настройках до выхода; иначе — через поддержку.
3. Письма на скрытые адреса Apple не дойдут, пока домен не зарегистрирован у Apple.
4. В приложениях нет удаления аккаунта (App Store 5.1.1(v)); при его добавлении
   нужен отзыв токенов Apple через REST API.
5. Кнопка Google использует нейтральную иконку «G»; для релиза поставить
   официальный ассет по брендбуку Google.
6. Лимиты запросов в памяти одного инстанса (как и раньше); при масштабировании — Redis.
7. Android без Google Play services — только вход по почте.
8. 80 старых узбекских строк Android — английский fallback (не связано со входом).
9. Локальные служебные файлы наблюдателя сборки (`.claude-*`, `.run-lint`) остались
   на диске, они в `.gitignore` — можно удалить вручную.
