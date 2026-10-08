# Firebase: настройка для production-сборок

Push-уведомления AI Reply идут через Firebase Cloud Messaging (FCM) на обеих платформах. iOS получает их через APNs, FCM пересылает их сам. Код готов. Не хватает только конфигурационных файлов и ключей Firebase, которые есть у владельца проекта.

> **Статус: настройка не завершена (pending setup), это не дефект.** Пока файлов нет:
> - приложения собираются и работают;
> - push просто выключен: в настройках нет раздела уведомлений, запроса разрешения нет;
> - сервер отдаёт `features.push_notifications = false`, автоматические события записываются как `skipped`.
>
> Добавлять файлы можно в любой момент, исходный код менять не нужно.

Ничего из перечисленного ниже **не коммитится**. Пути уже в `.gitignore`.

| Что | Куда положить | Кто читает |
|---|---|---|
| `google-services.json` (Android-приложение `kz.yerek.aireply`) | `AI-Reply-Android/app/google-services.json` | Gradle-плагин Google Services |
| `GoogleService-Info.plist` (iOS-приложение `kz.ai-reply.reply.keyboard.keyboard`) | `AI-Reply/Config/Firebase/GoogleService-Info.plist` | шаг сборки «Firebase config» в таргете `AIReply` |
| JSON сервисного аккаунта (Firebase Admin, для сервера) | **вне репозитория** на сервере, например `/srv/ai-reply/secrets/firebase-service-account.json` | бэкенд через `FIREBASE_SERVICE_ACCOUNT_FILE` |
| APNs Auth Key `.p8` | загружается в Firebase Console, на сервер не нужен | Firebase |

## 1. Проект Firebase

1. Firebase Console → создать проект (один на обе платформы) или открыть существующий.
2. Project settings → Cloud Messaging: должен быть включён **Firebase Cloud Messaging API (V1)**. Legacy server key не нужен.
3. **Не подключать** Google Analytics for Firebase: приложения используют только FCM, и ярлыки приватности App Store и Google Play исходят из этого.

## 2. Android

1. Firebase → Add app → Android, package name **`kz.yerek.aireply`**. Debug и release используют один и тот же applicationId. Для Google-входа добавить SHA-1/SHA-256 upload-ключа и ключа Play App Signing.
2. Скачать `google-services.json` и положить в **`AI-Reply-Android/app/google-services.json`**.
3. Больше ничего не нужно. `app/build.gradle.kts` применяет плагин `com.google.gms.google-services` (версия в `gradle/libs.versions.toml`) только если файл существует. Firebase инициализируется при старте, а `PushSupport` проверяет `FirebaseApp.getApps` перед обращением к Messaging. Токен запрашивается только после согласия с документами (`firebase_messaging_auto_init_enabled=false` в манифесте).
4. Без файла release-сборка печатает `WARNING: Firebase setup pending: …` и собирается без push.
5. Отдельно от Firebase: «Продолжить с Google» требует `aireply.googleWebClientId` (OAuth-клиент типа Web) в `local.properties`, `~/.gradle/gradle.properties`, `-Paireply.googleWebClientId=…` или `GOOGLE_WEB_CLIENT_ID`. Без него кнопка Google скрыта.

## 3. iOS

1. Firebase → Add app → Apple, bundle ID **`kz.ai-reply.reply.keyboard.keyboard`** (основное приложение, не клавиатура: расширение push не получает). Team ID: `2PK6339Q47`.
2. Скачать `GoogleService-Info.plist` и положить в **`AI-Reply/Config/Firebase/GoogleService-Info.plist`**. Добавлять файл в Xcode-проект не нужно. Шаг сборки «Firebase config» (последний в таргете `AIReply`):
   - копирует файл в приложение, если он есть;
   - проверяет, что это корректный plist и что его `BUNDLE_ID` совпадает с bundle id приложения (иначе сборка падает с понятной ошибкой);
   - без файла печатает `note: Firebase is not configured yet …` и убирает старую копию из бандла.
3. Инициализация уже в коде: `AIReply/Features/Notifications/FirebasePush.swift`. Он берёт опции из `GoogleService-Info.plist` в бандле, а если файла нет, из build settings `FIREBASE_GOOGLE_APP_ID`, `FIREBASE_GCM_SENDER_ID`, `FIREBASE_API_KEY`, `FIREBASE_PROJECT_ID` в `project.yml` / `project.pbxproj`. Это альтернатива файлу, использовать что-то одно. Опции проверяются до `FirebaseApp.configure`, поэтому неполный конфиг не приводит к падению. Свизлинг app delegate и автоматический токен выключены (`FirebaseAppDelegateProxyEnabled=false`, `FirebaseMessagingAutoInitEnabled=false`). Токен появляется только после согласия и разрешения на уведомления.
4. Apple Developer → Keys → создать ключ с **Apple Push Notifications service (APNs)** и скачать `AuthKey_<KEY_ID>.p8` (скачивается один раз). Firebase → Project settings → Cloud Messaging → Apple app configuration → загрузить ключ с Key ID и Team ID.
5. Apple Developer → Identifiers → `kz.ai-reply.reply.keyboard.keyboard` → включить Push Notifications. В `Config/AIReply.entitlements` уже стоит `aps-environment = development`. При экспорте для App Store или TestFlight Xcode подписывает его как `production`.
6. `AIREPLY_PUSH_NOTIFICATIONS` в `project.yml` уже `YES`. Push включается сам, когда есть опции Firebase и сервер сообщает `features.push_notifications = true`.

## 4. Сервер (бэкенд)

1. Firebase → Project settings → Service accounts → **Generate new private key**. Это JSON сервисного аккаунта Firebase Admin, то есть **секрет**.
2. Положить файл на сервер **вне репозитория и вне Docker-образа**, например `/srv/ai-reply/secrets/firebase-service-account.json`, с правами `chmod 600`. В `.gitignore` уже есть `ai-reply-back-end/secrets/` и `*firebase-adminsdk*.json`, в `.dockerignore` — то же самое: файл не попадёт ни в git, ни в образ, даже если оказался рядом с кодом.
3. Передать файл серверу одним из двух способов (оба уже поддерживаются в `config/config.go`, задать можно только один):
   - **Файл:** `FIREBASE_SERVICE_ACCOUNT_FILE=/run/secrets/firebase-service-account.json`. В Docker файл монтируется только для чтения. Раскомментировать строку в `docker-compose.yml` (секция `volumes` сервиса):
     ```yaml
     - /srv/ai-reply/secrets/firebase-service-account.json:/run/secrets/firebase-service-account.json:ro
     ```
     Не раскомментируйте строку, пока файла на хосте нет: Docker создаст на его месте пустую папку.
   - **Три переменные:** `FIREBASE_PROJECT_ID`, `FIREBASE_CLIENT_EMAIL`, `FIREBASE_PRIVATE_KEY` (поля `project_id`, `client_email`, `private_key` из того же JSON). Ключ передаётся одной строкой с `\n` в кавычках или в base64.
4. Включить отправку: `PUSH_NOTIFICATIONS_ENABLED=true`.
5. Проверки при старте:
   - файл, который нельзя прочитать или который не является service account JSON, останавливает запуск с понятной ошибкой;
   - одновременно файл и три переменные — тоже ошибка;
   - только одна-две из трёх переменных — ошибка;
   - содержимое ключа в лог не пишется.

## 5. Проверка после настройки

1. Сервер: в логе при старте `push` готов. `GET https://ai-reply.kz/api/v1/config` отдаёт `features.push_notifications: true` и `features.installations: true`.
2. Android: сборка без `WARNING: Firebase setup pending`. На устройстве: согласие → вход → карточка «Уведомления» → разрешить.
3. iOS (реальный iPhone, симулятор APNs-токен не получит): в логе сборки нет `note: Firebase is not configured yet`. Согласие → вход → разрешить уведомления.
4. Админка → Уведомления → тестовая кампания на свой аккаунт (категория «Новости»: она теперь по умолчанию выключена, сначала включите её в настройках приложения).
5. Подробности доставки, повторов и категорий: `docs/notifications.md`.

## 6. Чего делать не нужно

- Не коммитить `google-services.json`, `GoogleService-Info.plist`, JSON сервисного аккаунта и `.p8`.
- Не вставлять ключи в исходный код, `project.yml`, `build.gradle.kts` или `.env.example`.
- Не подменять файлы «заглушками»: без настоящих файлов push корректно выключен, а заглушка либо уронит инициализацию, либо даст неработающий push.
