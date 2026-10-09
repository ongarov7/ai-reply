# Firebase release checklist — 2026-10-08

Статус: **локальные клиентские конфигурации готовы; production push и релиз BLOCKED**.
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
| Apple Push Notifications capability | Выключена в Apple Developer; отдельный запрос разрешения ожидает ответа |
| APNs Sandbox / Production | Keys в Apple Developer отсутствуют, оба Firebase-слота пусты; запрос создания и загрузки ожидает ответа |
| Sign in with Apple | Capability включена; ошибка входа на iPhone TestFlight ещё не диагностирована |
| Notification permissions | Существующие flows сохранены; реальная проверка Android 13+ и iPhone не выполнена |
| Backend regressions | `go test ./... -count=1 -race`: 17 пакетов OK |
| Android regressions | 511 tests, 0 failures/errors; release Kotlin OK; lint 0 errors; Google Services processing OK |
| iOS regressions | `xcodebuild test`: TEST SUCCEEDED, 460 passed, 0 failed/skipped; Xcode 26.4.1, iPhone 17e simulator iOS 26.4.1; настоящий plist присутствует в app bundle |
| Live push / logout / account deletion | Нужны отдельные тестовые устройства после настройки APNs/backend; живые кампании не отправлялись |
| Backend P1 review | Legacy consent bypass и неточный privacy text остаются блокерами; см. recovery-раздел аудита |
| Следующий iOS build number | В App Store Connect номер 5 уже занят; перед будущей загрузкой выбрать >5, сейчас версия не менялась |
| App Store / Google Play | Поля приватности, production config, оператор/контакт и проверка устройств остаются; APK/AAB/IPA, upload и публикация не выполнялись |

## Передача на второй ноутбук

1. Сохранить незакоммиченную работу Claude и проверить его историю; не заменять её этим checkout.
2. На чистой ветке `notification`: `git fetch origin` → `git pull --ff-only origin notification`.
3. Если Claude работает в другой ветке, изучить Firebase-коммит и перенести его через `git cherry-pick <commit>` с проверкой конфликтов. Не применять force push или reset.
4. Запустить `python3 tools/validate_firebase_config.py` из корня. Это локальная проверка идентификаторов и подключения, не тест доставки.
5. Серверный JSON и APNs `.p8` переносить отдельно защищённым каналом. Их нет в Git. Не создавать новые ключи ради переноса уже существующих.

Для следующего инженера: сначала сопоставить отсутствующие Claude-коммиты и полный список 21 находки с текущим кодом; закрыть P1, затем P2/P3 по реальной тяжести. Не считать исторические результаты тестов проверкой новых изменений.
