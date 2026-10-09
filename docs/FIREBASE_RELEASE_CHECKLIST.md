# Firebase release checklist — updated 2026-10-09

Статус: **production APNs настроен; финальная iOS 1.0 (6) из main обработана и доступна в AI-Reply Internal со статусом «Тестируется»; production backend и реальные устройства ещё требуют проверки**.
Рабочая ветка: `notification`. Основной мануал: [FIREBASE_SETUP.md](FIREBASE_SETUP.md).

| Проверка | Статус / следующий шаг |
|---|---|
| Существующий проект | `ai-reply-4bf8f`, Sender ID `307300959376`, Spark; billing не менялся |
| Android registration | Готово: `kz.yerek.aireply`; настоящий `app/google-services.json` включён в Git |
| iOS registration | Готово: `kz.ai-reply.reply.keyboard.keyboard`; настоящий `Config/Firebase/GoogleService-Info.plist` включён в Git |
| Firebase SDK / подключение конфигов | Уже реализованы Claude; повторной интеграции нет; валидатор проходит |
| FCM HTTP v1 | Enabled; legacy API Disabled |
| Backend service account | `ai-reply-fcm-sender@ai-reply-4bf8f.iam.gserviceaccount.com`; только `roles/firebasecloudmessaging.admin` |
| Backend JSON | Создан с разрешения владельца, локально `ai-reply-back-end/secrets/firebase-service-account.json`, права `600`; Git и Docker исключают secrets |
| Server deployment | Не выполнялся; JSON ещё не переносился на сервер; нужны mount + `FIREBASE_SERVICE_ACCOUNT_FILE` + `PUSH_NOTIFICATIONS_ENABLED=true` |
| Apple Team / App ID | Проверены в Developer portal: `2PK6339Q47` / основной bundle ID совпадают |
| Apple Push Notifications capability | Включена с разрешения владельца 9 октября; новый development profile содержит `aps-environment=development`, подписанный archive создан |
| APNs Production | Владелец зарегистрировал и скачал ключ `J2YCR8N32P`: только APNs, Production / Topic Specific / основной bundle. Защищённая копия `AI-Reply/Config/Secrets/APNs/AuthKey_J2YCR8N32P.p8`, права 600, вне Git. Firebase UI подтвердил production-ключ и Team `2PK6339Q47`; реальная доставка ещё не проверена |
| APNs Sandbox | Отдельный Sandbox-ключ ещё не настроен. Production-ключ используется для TestFlight, не для Debug push |
| Sign in with Apple | Capability включена и присутствует в подписанном archive; в присланном server env неверный `APPLE_CLIENT_ID`, требуется `kz.ai-reply.reply.keyboard.keyboard`, затем тест на iPhone |
| Google iOS sign-in | Настоящий iOS OAuth client и reversed scheme встроены; backend audience должен быть настроен владельцем. Google client UI показывает App Check metrics / кнопку Enforce: enforcement не включён. Код запрашивает только базовый вход (`additionalScopes: nil`); для него Google допускает пользователей вне test-user list даже в Testing |
| Notification permissions | Существующие flows сохранены; реальная проверка Android 13+ и iPhone не выполнена |
| Backend regressions | 9 октября Codex: `go test ./... -count=1`, 17 тестируемых пакетов OK; предыдущий race-результат Claude сохранён в аудите |
| Android regressions | Claude: 512 tests, 0 failures; release Kotlin / lint / Google Services OK. Исторический результат; Android-код в текущем продолжении не менялся |
| iOS regressions | 9 октября Codex: `xcodebuild test`, 463 passed, 0 failed; iPhone 17 Pro iOS 26.4.1. Firebase plist, OAuth ID и URL scheme подтверждены в собранном app |
| Live push / logout / account deletion | Нужны отдельные тестовые устройства после настройки APNs/backend; живые кампании не отправлялись |
| Backend P1 review | Исправления Claude уже в `b111181`; повторные backend-тесты прошли. Требуется отдельный deployment владельцем с production env; на live server изменения не применялись Codex |
| Текущий iOS build number | 1.0 (6), одинаковый у приложения и расширения. App Store Connect: обработка завершена, сборка `9c00493c-0daf-40b4-b1ec-a7714730e206` в существующей AI-Reply Internal, статус «Тестируется», 90 дней. Создана 9 октября 14:22 |
| Distribution signing | Финальный archive из чистого main `468fd14` и App Store export успешны; strict-подписи приложения/расширения, profiles, App Group, keychain, Apple sign-in и production APNs проверены. Приватных ключей и `.env` в IPA нет |
| Live backend check | Только публичные GET: `/healthz` и `/api/v1/config` HTTP 200; env=staging, demo_mode=true, payment_mode=demo. Нет installations/push_notifications и других новых feature flags; deployment нового backend ещё необходим |
| App Store / Google Play | Поля приватности, production config, оператор/контакт и физические устройства ещё требуют проверки. Цель текущего этапа — TestFlight из финального main; public store review / release не выполнялись |

## Передача на второй ноутбук

1. Сохранить незакоммиченную работу Claude и проверить его историю; не заменять её этим checkout.
2. На чистой ветке `notification`: `git fetch origin` → `git pull --ff-only origin notification`.
3. Если Claude работает в другой ветке, изучить Firebase-коммит и перенести его через `git cherry-pick <commit>` с проверкой конфликтов. Не применять force push или reset.
4. Запустить `python3 tools/validate_firebase_config.py` из корня. Это локальная проверка идентификаторов и подключения, не тест доставки.
5. Серверный JSON и APNs `.p8` переносить отдельно защищённым каналом. Их нет в Git. Не создавать новые ключи ради переноса уже существующих.

Для следующего инженера: восстановленные Claude-коммиты и список 21 находки уже находятся в репозитории. Актуальный прогресс — в конце `STORE_RELEASE_AUDIT.md`; server env diff и порядок deployment — в `BACKEND_DEPLOYMENT_CHECKLIST.md`. Не применять старую временную recovery patch поверх готового `b111181`. Не считать исторические тесты проверкой новых изменений.

Google's [Audience documentation](https://support.google.com/cloud/answer/15549945?hl=en) documents the Testing exception for basic Sign in with Google. App Check [enforcement is a separate action](https://developers.google.com/identity/sign-in/ios/appcheck/enable-enforcement); it was not enabled during this continuation.
