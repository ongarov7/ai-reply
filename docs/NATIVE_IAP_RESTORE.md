# Нативные покупки iOS — подготовка сборки 16

Состояние на 30 октября 2026 года, 23:34 +05:00. Ветка `Payment`.

Платёжная витрина восстановлена выборочно из `0386421`, с сохранением нового Home, балансов и исправлений безопасности сборок 14/15. Полного отката этих сборок не было. Android и аудиокод не изменялись.

## Поведение приложения

- Покупки выполняются через StoreKit 2. Base/Pro представлены автопродлеваемыми подписками с выбором месяца или года; разовые пакеты находятся отдельно. Цена и период покупки берутся из совпавшего продукта App Store. Несоответствие цены блокирует кнопку; отсутствующий StoreKit-продукт не заменяется фиктивной покупкой.
- Текущий баланс остаётся серверным: welcome, купленные и административные ответы показываются отдельно; безлимит отображается как `∞`. После покупки и при возврате приложения на передний план обновляется `/me`.
- Витрина содержит восстановление подписок и системное управление подписками Apple. Отмена автопродления сохраняет доступ до конца оплаченного периода. Опциональное поле сервера `auto_renews` позволяет отличить дату продления от окончания доступа; старый ответ без поля продолжает декодироваться.
- `appAccountToken` связывает покупку с аккаунтом. Подписанная транзакция отправляется серверу; `finish()` выполняется после подтверждённого начисления или подтверждённого отзыва. Pending, отмена пользователем и ошибки сервера не превращаются в успешную покупку. Очереди unfinished и StoreKit updates сохранены.
- Начало выхода синхронно отключает новые покупки до ожидания ответа logout. Сохранённая витрина не позволяет начать покупку после выхода. Транзакция, пришедшая во время выхода, ждёт аккаунт; уже подтверждённое сервером начисление A не обновляет интерфейс другого пользователя.
- Старый `AccountModel.choosePlan` с create/confirm checkout остаётся запрещён отдельным `legacyCheckoutAvailableInThisBuild=false`. Он не используется вместо StoreKit.

## Сайт и уведомления

Карточка Home переименована в «О сервисе AI Reply» / «Как работает сервис и поддержка» во всех четырёх локалях. Она открывает только информационный `/app` либо `/sandbox/app`, построенный из канонического backend URL, через существующий браузер приложения. Авторизация на сайте не передаётся.

Вызов `/web/payment-handoff` и платёжные SSO-маршруты удалены из iOS. Старый тип `web_topup_offer` всегда игнорируется, включая уже доставленные уведомления. `/payment`, `/account`, checkout-ссылки, произвольные домены, redirect-параметры и handoff fragments не принимаются. Обычные уведомления, настройки marketing consent и legal/support-маршруты сохранены. Нативный `aireply://purchase` ведёт в StoreKit-витрину при доступном IAP.

После серверного развёртывания `/app` и `/sandbox/app` возвращают 200. Страница `/sandbox/app?lang=ru` проверена в браузере: информация о сервисе, документы, поддержка и ссылка App Store; платёжных ссылок и переходов в продающий кабинет нет.

## Проверки

- Полный iOS suite завершён: **700 passed, 0 failed, 0 skipped**. Результат `Passed` независимо прочитан через `xcresulttool` из `/tmp/ai-reply-native-iap-16.xcresult`. Лог: `/tmp/ai-reply-native-iap-16-tests.log`.
- Восстановлены пять `SubscriptionDisplayTests` внутри `PurchaseStoreTests.swift`. Сохранены проверки price mismatch, подтверждения сервером, pending, restore и смены аккаунта; добавлены проверки выхода и сохранения транзакций. Удалённые SSO-переходы заменены проверками запрета внешней оплаты и безопасного информационного URL. Число тестов отличается от 707 в сборке 15 из-за замены отменённого сценария.
- Parse всех изменённых Swift, `git diff --check`, четыре локали и JSON fixtures для optional renewal прошли. В логе остаются предупреждение о лишнем `await` в `AccountSession.swift:45` и примечание о фазе Firebase; ошибок тестов нет.
- Реальная покупка Apple, продление, возврат средств и доставка App Store Server Notifications этим прогоном не проверялись. Release-архив 1.2.0 (16) собран, `codesign --verify --deep --strict` пройден; версия 16 и sandbox API подтверждены у приложения и расширения. Загрузка во внутренний TestFlight принята Apple в 23:39:19 +05:00 (`Upload succeeded`); обработка и видимость сборки в группе ещё проверяются.

## Внешние условия выпуска

1. App Store Connect должен содержать фактические продукты, правильные периоды, уровни одной subscription group, доступность и сохранённые цены. Исторически подтверждён пакет `kz.aireply.pack30`; остальные SKU и текущие статусы проверяет владелец. Проектируемые идентификаторы не считаются созданными продуктами. Backend mapping должен совпадать с App Store; интерфейс использует фактическую StoreKit-цену.
2. Статус Paid Apps Agreement, сведения юридического лица, налоговые и банковские данные подтверждает Account Holder. Повторная проверка Chrome 30 октября 2026 года в 23:31 +05:00 снова показала договор «Новое» и требование обновить юридические данные. Форма открыта владельцу; сведения и принятие договора ожидают его действий. Для продажи приложений/IAP и отправки их обновлений Apple требует действующий платный договор. [Официальные условия договоров](https://developer.apple.com/help/app-store-connect/manage-agreements/sign-and-update-agreements/).
3. Small Business Program — отдельная заявка Account Holder. Apple предусматривает комиссию 15% для подходящих участников и требует актуальный Paid Apps Agreement и раскрытие связанных developer accounts. Восстановление StoreKit само по себе не означает вступление или применение ставки. Форма заявки открыта под Team 2PK6339Q47; на обязательном первом вопросе она требует принятия Paid Applications Agreement и не даёт продолжить. Заявка не отправлена, принятие условий, одобрение и дата действия ставки не подтверждены. [Официальная программа Apple](https://developer.apple.com/app-store/small-business-program/).
4. Текущий TestFlight использует `https://ai-reply.kz/sandbox` и отдельную серверную среду. Клиент отправляет JWS в свой настроенный backend; автоматического переключения базы по типу receipt нет. До публичного выпуска нужно явно проверить обработку Sandbox-транзакций App Review при production API и сохранение привязки к правильному аккаунту. Нельзя считать этот путь проверенным по успешной сборке TestFlight.

Никаких реальных списаний, публичной отправки приложения или принятия договоров в рамках описанных проверок не выполнялось.

## Additional App Store Connect checkpoint

At 23:40–23:43 +05:00, `kz.aireply.pack30` / Apple ID `6828802544` was rechecked: Kazakhstan price 100 KZT, availability 1/175, Russian localization saved, Prepare for Submission. Group 22528719 contained only Pro Monthly `kz.aireply.pro.monthly` / 6828808586 with a one-month period. Created Pro Yearly draft `kz.aireply.pro.yearly` / `6829001596`, one year; its pricing, availability, localization and backend mapping remain incomplete. No price was saved in the open wizard.

The owner subsequently confirmed no associated accounts/control relationships and aggregate 2025 proceeds not above USD 1 million. All four associated-account answers are now No. The application has not been submitted: Paid Apps Agreement still shows New, and saving the existing legal-entity information triggered Apple's SMS two-factor verification. The owner was asked to enter the code directly in Chrome; no code or new authentication credential was requested in chat.

Pro Yearly now has saved initial Kazakhstan pricing of 29,990 KZT, Kazakhstan-only upfront-year availability (1/175), and Russian display name/description. Monthly installments and Family Sharing were not enabled. Base Monthly `kz.aireply.base.monthly` / `6829003401` and Base Yearly `kz.aireply.base.yearly` / `6829003698` were created as drafts in group 22528719. The extended Apple price list contains 599 KZT for monthly Base; it offers 5,990 KZT rather than 5,999 KZT for annual Base. The owner explicitly chose not to enable annual Base for now; its price was not saved or published.

Before enabling any subscription mapping, configure individual App Store purchases only and disallow multiseat purchases. Apple's new default also permits Apple Business/School and multiseat; the current backend deliberately binds one original transaction to one user and has no seat model. [Apple purchase options](https://developer.apple.com/help/app-store-connect/manage-subscriptions/manage-purchase-options-for-auto-renewable-subscriptions).

## Серверная часть текущего изменения

Исправлена обработка REFUND/REVOKE/REFUND_REVERSED: используется проверенное подписанное Apple время, состояние сохраняется и для ещё не зарегистрированной транзакции; изменение баланса и порядка события атомарно. Миграция 0027 добавляет отдельную таблицу и не меняет существующие покупки/балансы; порядок ранее обработанных исторических уведомлений не восстанавливается. Полный `go test -count=1 ./...` и race-регрессии ApplePackLifecycle прошли; независимый review блокеров не выявил.

Перед развёртыванием сохранены согласованные SQLite backup обеих БД и проверен integrity_check. BCC checkout и web-offer генерация выключены на сервере; прежняя реализация сохранена. `/healthz` и `/sandbox/healthz` после развёртывания `21971f7` отвечают 200, таблица миграции 0027 подтверждена в обеих БД; sandbox config сообщает `iap_ios=true`. Dispatcher повторно проверяет отключённый web-offer флаг перед FCM: ожидающие предложения пропускаются, обычные push сохранены.
