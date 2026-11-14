# Google Play — закрытое тестирование платежей

14 ноября 2026. Package `kz.yerek.aireply`, версия **1.2.1 / code 5**.
На момент проверки в Console максимальный загруженный code — 4. Перепроверьте перед загрузкой.

Google Group `google-play-dev-test@googlegroups.com` подключена к существующему закрытому треку
**Google Group Testers** и License testing. Владелец аккаунта также является license tester.
Участники вступают в группу и принимают тестирование тем же Google-аккаунтом:
<https://play.google.com/apps/testing/kz.yerek.aireply>.

## Сборка и подпись на Mac с существующим upload key

```sh
git pull --ff-only origin main
cd AI-Reply-Android
./scripts/build-play-sandbox.sh "/absolute/path/existing-upload.jks" EXISTING_ALIAS
```

Подставьте путь и alias имеющегося upload key. Пароли вводятся только в запросах терминала.
Скрипт использует JDK Android Studio или `JAVA_HOME`, собирает offline, явно выбирает sandbox,
проверяет фактический BuildConfig, подписывает и сравнивает upload certificate с известным
fingerprint. При отсутствии кеша зависимости сначала требуется получить обычной Gradle-сборкой.
Новый ключ или reset существующего ключа не нужны.

Результат: `app/build/AIReply-Sandbox-1.2.1-code5-signed.aab`.
Внутри должно быть название **AI Reply Sandbox**, backend **https://ai-reply.kz/sandbox**.
Обычная production-сборка по умолчанию использует основной API. Если мастер Android Studio
пересобирает AAB самостоятельно, он тоже должен получить `-Paireply.backend=sandbox`;
не заменяйте проверенный файл результатом сборки с другими настройками.

Загрузите результат **только** в закрытый трек Google Group Testers. Не продвигайте его в
production. Production остаётся на прежнем выпуске. Следующий публичный выпуск должен
использовать production backend и code **6 или выше**, если code 5 уже загружен в Play.

## Товары и настоящая проверка

| Тип | Цена | Результат |
|---|---:|---|
| Разовый пакет `kz.aireply.pack30` | 130 ₸ | 30 ответов |
| Разовый пакет `kz.aireply.pack500` | 990 ₸ | 500 ответов |
| Подписка `kz.aireply.pro.monthly` | 3 990 ₸/месяц | Безлимит на оплаченный период |
| Существующая годовая Pro | 39 990 ₸/год | Безлимит на оплаченный период |

Base Monthly на 1 000 ответов не включён. Пакеты не являются автопродлеваемыми подписками.
Sandbox получает только тестовые покупки Google и отдельные аккаунты/балансы.
Настройки iOS, BCC и основной каталог не меняются этим закрытым выпуском.

Используйте отдельный sandbox-аккаунт AI Reply и Google license-test аккаунт на устройстве.
В системном диалоге должны быть отметка тестовой покупки и тестовый платёжный инструмент.
Обычное членство в закрытом треке само по себе не отменяет реальные списания.
Проверьте покупку → серверное начисление → consume/ack → восстановление без дубля,
отмену и возврат. Google тестовые продления ускорены; обработка возврата пакета может
занять до 15 минут. Реальная сквозная покупка на устройстве пока не подтверждена.

Полный план и ограничения: [GOOGLE_PLAY_SANDBOX.md](../../../../ai-reply-back-end/docs/GOOGLE_PLAY_SANDBOX.md).
Официальные инструкции: [Google license testing](https://developer.android.com/google/play/billing/test),
[подпись AAB](https://developer.android.com/build/building-cmdline).
