# Firebase: настройка для production-сборок

Push-уведомления AI Reply идут через Firebase Cloud Messaging (FCM) на обеих платформах; на iOS FCM доставляет их через APNs.

## Текущее состояние — 8 октября 2026

- Использован существующий проект **AI-Reply**, ID **`ai-reply-4bf8f`**, номер / Sender ID **`307300959376`**, тариф **Spark**. Новый проект и billing не создавались.
- Зарегистрированы Android **`kz.yerek.aireply`** (`1:307300959376:android:250a4b3bef59d0129b4415`) и основное iOS-приложение **`kz.ai-reply.reply.keyboard.keyboard`** (`1:307300959376:ios:eba12e693c10e0e19b4415`, App Store ID `6818570150`).
- Настоящие клиентские `google-services.json` и `GoogleService-Info.plist` скачаны из Firebase Console, проверены и **включены в Git по запросу владельца**. Они переносятся на второй ноутбук обычным `git pull`.
- FCM HTTP v1 **Enabled**, legacy messaging **Disabled**. Firebase Authentication, базы данных и Analytics для этой интеграции не включались; в iOS-конфиге Analytics отключён.
- Apple Developer проверен: Team `2PK6339Q47`, основной App ID совпадает; Sign in with Apple включён, Push Notifications выключен, список Keys пуст. APNs development / production ключи пока не загружены. **iPhone push ещё не готов**.
- Для service account `ai-reply-fcm-sender` назначена только `roles/firebasecloudmessaging.admin`; JSON создан с разрешения владельца и сохранён локально как `ai-reply-back-end/secrets/firebase-service-account.json` (каталог `700`, файл `600`, игнорируется Git). На сервер его ещё не переносили.
- Backend не развёртывался, production credentials не менялись, настоящие кампании не отправлялись. Публичный production `/api/v1/config` пока не объявляет push-функции.

Проверка после получения изменений на другом ноутбуке:

```bash
git fetch origin
git switch notification
git pull --ff-only origin notification
python3 tools/validate_firebase_config.py
```

Если на втором ноутбуке у Claude есть незакоммиченная работа, сначала сохранить её в его ветке или отдельном патче и изучить diff перед интеграцией. Не применять `reset --hard`, не заменять весь проект и не сливать вслепую. Скрипт проверяет обе конфигурации, project / sender ID, идентификаторы приложений и текущие подключения Gradle/Xcode; API keys не печатает.

Клиентские API keys идентифицируют Firebase-проект и не заменяют серверную авторизацию ([Firebase API keys](https://firebase.google.com/docs/projects/api-keys)). JSON сервисного аккаунта и Apple `.p8` — приватные ключи; они **не переносятся через Git**.

| Что | Куда положить | Кто читает |
|---|---|---|
| `google-services.json` (Android-приложение `kz.yerek.aireply`) | `AI-Reply-Android/app/google-services.json` | Gradle-плагин Google Services |
| `GoogleService-Info.plist` (iOS-приложение `kz.ai-reply.reply.keyboard.keyboard`) | `AI-Reply/Config/Firebase/GoogleService-Info.plist` | шаг сборки «Firebase config» в таргете `AIReply` |
| JSON сервисного аккаунта (FCM HTTP v1, для сервера) | **вне репозитория** на сервере, например `/srv/ai-reply/secrets/firebase-service-account.json` | бэкенд через `FIREBASE_SERVICE_ACCOUNT_FILE` |
| APNs Auth Key `.p8` | загружается в Firebase Console, на сервер не нужен | Firebase |

## 1. Проект Firebase

1. Открыть существующий проект [`ai-reply-4bf8f`](https://console.firebase.google.com/project/ai-reply-4bf8f/settings/general). Не создавать дубликат.
2. Project settings → Cloud Messaging: должен быть включён **Firebase Cloud Messaging API (V1)**. Legacy server key не нужен.
3. **Не подключать** Google Analytics for Firebase: приложения используют только FCM, и ярлыки приватности App Store и Google Play исходят из этого.

## 2. Android

1. Приложение уже зарегистрировано: package name **`kz.yerek.aireply`**. Debug и release используют один и тот же applicationId. Для Google-входа добавить SHA-1/SHA-256 upload-ключа и ключа Play App Signing.
2. Конфиг уже находится в **`AI-Reply-Android/app/google-services.json`** и включён в Git.
3. Больше ничего не нужно. `app/build.gradle.kts` применяет плагин `com.google.gms.google-services` (версия в `gradle/libs.versions.toml`) только если файл существует. Firebase инициализируется при старте, а `PushSupport` проверяет `FirebaseApp.getApps` перед обращением к Messaging. Токен запрашивается только после согласия с документами (`firebase_messaging_auto_init_enabled=false` в манифесте).
4. Без файла release-сборка печатает `WARNING: Firebase setup pending: …` и собирается без push.
5. Отдельно от Firebase: «Продолжить с Google» требует `aireply.googleWebClientId` (OAuth-клиент типа Web) в `local.properties`, `~/.gradle/gradle.properties`, `-Paireply.googleWebClientId=…` или `GOOGLE_WEB_CLIENT_ID`. Без него кнопка Google скрыта.

## 3. iOS

1. Приложение уже зарегистрировано: bundle ID **`kz.ai-reply.reply.keyboard.keyboard`** (основное приложение, не клавиатура: расширение push не получает). Team ID: `2PK6339Q47`.
2. Конфиг уже находится в **`AI-Reply/Config/Firebase/GoogleService-Info.plist`** и включён в Git. Добавлять файл в Xcode-проект не нужно. Шаг сборки «Firebase config» (последний в таргете `AIReply`):
   - копирует файл в приложение, если он есть;
   - проверяет, что это корректный plist и что его `BUNDLE_ID` совпадает с bundle id приложения (иначе сборка падает с понятной ошибкой);
   - без файла печатает `note: Firebase is not configured yet …` и убирает старую копию из бандла.
3. Инициализация уже в коде: `AIReply/Features/Notifications/FirebasePush.swift`. Он берёт опции из `GoogleService-Info.plist` в бандле, а если файла нет, из build settings `FIREBASE_GOOGLE_APP_ID`, `FIREBASE_GCM_SENDER_ID`, `FIREBASE_API_KEY`, `FIREBASE_PROJECT_ID` в `project.yml` / `project.pbxproj`. Это альтернатива файлу, использовать что-то одно. Опции проверяются до `FirebaseApp.configure`, поэтому неполный конфиг не приводит к падению. Свизлинг app delegate и автоматический токен выключены (`FirebaseAppDelegateProxyEnabled=false`, `FirebaseMessagingAutoInitEnabled=false`). Токен появляется только после согласия и разрешения на уведомления.
4. С отдельным разрешением владельца Apple Developer → Keys → создать **Apple Push Notifications service (APNs)** ключи для Sandbox и Production с областью **Topic Specific** только для bundle ID приложения, скачать `AuthKey_<KEY_ID>.p8` (каждый скачивается один раз). Firebase → Project settings → Cloud Messaging → Apple app configuration → загрузить их в соответствующие слоты с Key ID и Team ID. Новые ключи имеют область и окружение; не считать один ключ универсальным ([Apple APNs keys](https://developer.apple.com/help/account/keys/create-a-private-key/)).
5. Apple Developer → Identifiers → `kz.ai-reply.reply.keyboard.keyboard` → включить Push Notifications. В `Config/AIReply.entitlements` уже стоит `aps-environment = development`. При экспорте для App Store или TestFlight Xcode подписывает его как `production`.
6. `AIREPLY_PUSH_NOTIFICATIONS` в `project.yml` уже `YES`. Push включается сам, когда есть опции Firebase и сервер сообщает `features.push_notifications = true`.

## 4. Сервер (бэкенд)

1. Для backend подготовлен отдельный service account `ai-reply-fcm-sender@ai-reply-4bf8f.iam.gserviceaccount.com`. Ему назначена только роль **Firebase Cloud Messaging API Admin** (`roles/firebasecloudmessaging.admin`). JSON-ключ — **секрет**, его нельзя добавлять в клиентские файлы или Git. Статус роли и локального ключа указан в `FIREBASE_RELEASE_CHECKLIST.md`.
2. Локальный файл `ai-reply-back-end/secrets/firebase-service-account.json` уже создан и игнорируется Git. Перенос на второй ноутбук или сервер — отдельно защищённым каналом (например SSH/SCP после проверки хоста), без вставки ключа в чат. Положить файл на сервер **вне репозитория и вне Docker-образа**. Исходное имя `ai-reply-4bf8f-31db4f3ee80d.json` можно сохранить: на сервере создать `/srv/ai-reply/secrets` и загрузить файл туда. Для существующего Dockerfile файл должен читаться UID `10001`: на обычном rootful Linux Docker выполнить `chown 10001:10001 /srv/ai-reply/secrets/ai-reply-4bf8f-31db4f3ee80d.json` и `chmod 600` для этого файла; каталог оставить root-owned с правами `700`. При rootless/user-namespace remapping права нужно согласовать с отображением UID. В `.gitignore` и `.dockerignore` уже исключены secrets и этот исходный JSON: он не должен попасть ни в Git, ни в образ.
3. Передать файл серверу одним из двух способов (оба уже поддерживаются в `config/config.go`, задать можно только один):
   - **Файл:** `FIREBASE_SERVICE_ACCOUNT_FILE=/run/secrets/firebase-service-account.json`. В Docker файл монтируется только для чтения. После подтверждения владельцем загрузки JSON блок уже включён в `docker-compose.yml` (секция `volumes` сервиса, существующий том базы сохранён). Файл на хосте должен существовать до запуска:
     ```yaml
     - type: bind
       source: /srv/ai-reply/secrets/ai-reply-4bf8f-31db4f3ee80d.json
       target: /run/secrets/firebase-service-account.json
       read_only: true
       bind:
         create_host_path: false
     ```
     `create_host_path: false` запрещает создавать пустую папку вместо отсутствующего файла. Проверка конфигурации без вывода секретов: `docker compose config --quiet`. Полная последовательность подготовки и проверки нового контейнера описана в `docs/BACKEND_DEPLOYMENT_CHECKLIST.md`; сервер Codex не изменял.
   - **Три переменные:** `FIREBASE_PROJECT_ID`, `FIREBASE_CLIENT_EMAIL`, `FIREBASE_PRIVATE_KEY` (поля `project_id`, `client_email`, `private_key` из того же JSON). Ключ передаётся одной строкой с `\n` в кавычках или в base64.
4. Включить отправку: `PUSH_NOTIFICATIONS_ENABLED=true`.
5. Проверки при старте:
   - файл, который нельзя прочитать или который не является service account JSON, останавливает запуск с понятной ошибкой;
   - одновременно файл и три переменные — тоже ошибка;
   - только одна-две из трёх переменных — ошибка;
   - содержимое ключа в лог не пишется.

## 5. Проверка после настройки

1. Сервер: в строке лога `server listening` поле `"push": true` (если `PUSH_NOTIFICATIONS_ENABLED=true`, а Firebase не настроен, будет предупреждение `push notifications are enabled but Firebase is not configured`). `GET https://ai-reply.kz/api/v1/config` отдаёт `features.push_notifications: true` и `features.installations: true`.
2. Android: сборка без `WARNING: Firebase setup pending`. На устройстве: согласие → вход → карточка «Уведомления» → разрешить.
3. iOS (реальный iPhone, симулятор APNs-токен не получит): в логе сборки нет `note: Firebase is not configured yet`. Согласие → вход → разрешить уведомления.
4. Админка → Уведомления → тестовая кампания на свой аккаунт (категория «Новости»: она теперь по умолчанию выключена, сначала включите её в настройках приложения).
5. Подробности доставки, повторов и категорий: `docs/notifications.md`.

## 6. Чего делать не нужно

- Не коммитить JSON сервисного аккаунта, `.p8` и реальные `.env`. Клиентские `google-services.json` и `GoogleService-Info.plist` уже включены в Git по запросу владельца.
- Не вставлять ключи в исходный код, `project.yml`, `build.gradle.kts` или `.env.example`.
- Не подменять файлы «заглушками»: без настоящих файлов push корректно выключен, а заглушка либо уронит инициализацию, либо даст неработающий push.

## 7. Краткий мануал для продолжения Claude

Поток доставки: **админка → авторизованный backend → FCM HTTP v1 → Android / APNs → iOS**. Backend уже использует сервисный аккаунт и короткоживущий OAuth access token в `internal/push/fcm.go`; менять этот рабочий путь на второй SDK-провайдер не требуется.

Android: Google Services plugin читает JSON, Firebase Messaging принимает токены и push. Android 13+ запрашивает `POST_NOTIFICATIONS` через существующий Activity Result flow; каналы, настройки категорий и переходы уже реализованы в `push/*`.

iOS: существующий build phase копирует plist только в основной app bundle; `FirebasePush.swift` конфигурирует Firebase. Swizzling выключен, поэтому app delegate вручную передаёт APNs-токен в Messaging. Разрешение запрашивает основное приложение через `UNUserNotificationCenter`, клавиатура Firebase не использует.

Обе платформы регистрируют установку через `POST /api/v1/installations`, обновляют FCM token и разрешения, отвязывают установку при logout / удалении аккаунта. Маркетинговые категории включаются только по выбору пользователя. Существующие тесты проверяют multi-device, token lifecycle, роли админки, invalid token, outbox и дедупликацию. Полный контракт: `docs/notifications.md`.

Перед живой проверкой: загрузить APNs ключи для нужных окружений ([Firebase iOS FCM setup](https://firebase.google.com/docs/cloud-messaging/ios/get-started)), безопасно смонтировать backend JSON и отдельно согласовать будущий deploy. Проверять только специально назначенные тестовые устройства. Тесты и компиляция не доказывают реальную доставку push.

Apple-вход в TestFlight — отдельная проверка: `Sign in with Apple` у App ID и entitlement приложения; backend `APPLE_CLIENT_ID` должен точно включать `kz.ai-reply.reply.keyboard.keyboard`. APNs-ключ используется для push, а ключ Sign in with Apple — для отзыва токенов при удалении аккаунта. Сам вход проверяет подписанный Apple ID token и nonce. Для установления причины сбоя нужен текст ошибки на iPhone либо безопасная строка server log `identity token rejected` с причиной, без токена.
