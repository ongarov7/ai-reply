# Google Play: изолированная проверка покупок

Google license testing использует тот же package, товары и Google Play Developer API,
что и реальные покупки. Отдельного «sandbox URL Google» нет. `/sandbox` — наша отдельная
база, пользователи, сессии и баланс. Клиент не выбирает доверенную среду: сервер проверяет
`purchaseType=0` у одноразовой покупки или `testPurchase` у подписки в ответе Google.
Обычная, промо- или pending покупка без признака license test отвергается до записи
покупки, выдачи ответов, consume и acknowledge. Подмена клиентского JSON этого не меняет.

Источники: [официальное тестирование Billing](https://developer.android.com/google/play/billing/test),
[ProductPurchase](https://developers.google.com/android-publisher/api-ref/rest/v3/purchases.products),
[SubscriptionPurchaseV2](https://developers.google.com/android-publisher/api-ref/rest/v3/purchases.subscriptionsv2),
[проверка и защита покупок](https://developer.android.com/google/play/billing/security).

## Настройка без включения продаж production

1. Разверните эту версию backend с существующими миграциями, включая `0038`.
   Само развёртывание не включает Google Play, Android-каталог и общий переключатель продаж.
2. Service account должен иметь app-scoped доступ к `kz.yerek.aireply`: просмотр финансовых
   данных и управление заказами/подписками. В его Cloud project включите Android Publisher API.
   JSON хранится вне репозитория и монтируется read-only. Можно явно указать уже смонтированный
   JSON с нужными правами; автоматического наследования Firebase/primary Google credentials нет.
3. В отдельном приватном env-файле, добавленном к `backend.env_file` через локальный Compose
   override после основного файла, задайте:

   ```dotenv
   BILLING_SANDBOX_ENABLED=true
   BILLING_SANDBOX_GOOGLE_ENABLED=true
   BILLING_SANDBOX_GOOGLE_SERVICE_ACCOUNT_FILE=/run/secrets/google-play-sandbox.json
   BILLING_SANDBOX_GOOGLE_PACKAGE_NAME=kz.yerek.aireply
   BILLING_SANDBOX_GOOGLE_TOKEN_SECRET=<отдельное стабильное случайное значение минимум 32 символа>
   BILLING_SANDBOX_GOOGLE_VOIDED_POLL=15m
   GOOGLE_PLAY_ACCEPT_TEST=false
   ```

   Последняя строка сохраняет отказ тестовых покупок в основном API. Не добавляйте primary
   `GOOGLE_PLAY_CLIENT_EMAIL`, `GOOGLE_PLAY_PRIVATE_KEY` или `GOOGLE_PLAY_SERVICE_ACCOUNT_FILE`
   ради sandbox. Если primary Google выключен, он останется выключенным. Не используйте
   `configure-billing-sandbox-env.py` для этой операции: он также меняет настройки BCC.
   Секрет генерируется один раз, например `openssl rand -hex 32`, и сохраняется приватно.
   Проверка `backend -check` не должна печатать содержимое JSON, purchase tokens или ключи.
4. `/sandbox` использует файл `*-sandbox` рядом с основной БД, отдельные JWT-ключи/issuer
   и admin cookie. Основные пользователи, каталог, оплаты и балансы не копируются.
   В `/sandbox/admin` отдельно задайте Google IDs, включите Android, опубликуйте каталог
   и включите покупки только в sandbox-БД. Для подписок также должен быть доступен `pack30`.
   В Play оставьте один auto-renew base plan на товар, без trial/специальных предложений;
   multi-quantity для `pack30` выключен.
5. Приложение для QA должно быть собрано с `-Paireply.backend=sandbox`, package остаётся
   `kz.yerek.aireply`. Оно использует `https://ai-reply.kz/sandbox` и отдельное локальное
   хранилище. Production APK/AAB продолжает использовать `https://ai-reply.kz`.

## Проверка настоящего тестового purchase token

Добавьте Google-аккаунт телефона в **Play Console → Settings → License testing**. Используйте
license-test аккаунт и тестовый платёжный инструмент, на диалоге должна быть отметка test.
Сам по себе internal/closed track не делает платёж бесплатным. Google разрешает license
testers sideload debug-сборки с совпадающим package; обычная установка через тестовый трек
тоже требует tester opt-in. Не публикуйте sandbox AAB в production.

Войдите в отдельный sandbox-аккаунт приложения. Оно само получит `account_token` и передаст
его через `setObfuscatedAccountId`, затем отправит token на
`POST /sandbox/api/v1/iap/google/purchases`. Скопированный production `account_token` или JWT
не подходит. Fake token не проверяет реальную интеграцию. Не передавайте реальные tokens в
чат, shell history, логи или репозиторий.

Проверьте на устройстве и в Play Console:

- **Always approves, pack30:** один test order, +30 только в sandbox, сервер выполнил consume;
  повторное открытие/восстановление не начисляет второй пакет. Покупка доступна повторно.
- **Always declines:** ни выдачи, ни consume/ack. **Slow test card:** pending даёт 202 и не
  начисляет; после перехода Google в PURCHASED клиент повторно отправляет покупку.
- **Base/Pro:** один grant, acknowledge отражён в Play; повтор не меняет баланс. Base
  начисляет следующий пакет за новый order, Pro даёт безлимит до оплаченного конца.
- **Отказ сервера/сети:** повтор после восстановления не дублирует выдачу. В license tests
  окно до автоматического возврата около трёх минут; проверяйте ack сразу, не через сутки.
  Sandbox worker делает retry и чтение подписки каждые 30 секунд; primary — раз в 15 минут.
- **Refund + revoke** тестового заказа в Play Console: после voided poll (до 15 минут при
  настройке выше) остаток именно этого пакета снят; чужие пакеты и production не изменились.
  В общей Google feed sandbox обрабатывает только уже известные локальные Test receipts.
- **Изоляция:** production JWT на `/sandbox` и sandbox JWT на основном API получают 401;
  purchase rows имеют `environment=Test` только в sandbox-БД. Основной баланс прежний.

RTDN пока нет: новый token при resubscribe из центра Play узнаётся при следующем открытии
приложения. Фоновые чтения работают только для уже известных токенов. Если не наблюдать
покупку целый расчётный период, промежуточные Base orders не восстанавливаются. Эти ограничения
не заменяют сквозную проверку. Автоматические тесты используют локальный fake Google и не
доказывают доступность реального service account/товаров/ack в Play.

## Проверки кода и отключение

```sh
go test ./config ./cmd/server -run TestBillingSandbox -count=1
go test -race ./internal/apptest -run TestGoogle -count=1
go test ./internal/iap/... ./internal/googleauth -count=1
```

Для остановки новых sandbox проверок снимите `BILLING_SANDBOX_GOOGLE_ENABLED` в отдельном
приватном файле и перезапустите backend обычным порядком. Primary настройки/БД не меняются.
Отключение worker не отменяет уже оплаченные тестовые покупки в Google: завершите QA заказов
и проверьте их состояние в Play Console перед отключением.
