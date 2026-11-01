# Выпуск из main: backend, сайт и Mac

На Linux работают API, личный кабинет, оплата и выдача DMG. Приложение работает
на Mac пользователя. Подпись и нотариализация выполняются на Mac; закрытый ключ
на сервер не переносится. Секретные значения владелец заполняет в серверном `.env`.

## Обновление сервера

Использовать существующий checkout, Compose project и том базы. Сначала сделать
консистентные SQLite-бэкапы штатной процедурой, включая committed WAL, и приватную
копию `.env`. Сохранить прежний Docker-образ для отката. При изменённом checkout
сначала сохранить правки; не применять `reset --hard` или `docker compose down -v`.

```sh
cd /home/ai-reply
git status --short
git fetch origin
git switch main
git pull --ff-only origin main
cd ai-reply-back-end
docker compose config --quiet
docker compose build backend
docker compose run --rm --no-deps backend -check
docker compose up -d --no-deps backend
docker compose ps backend
curl --fail https://ai-reply.kz/healthz
curl --fail https://ai-reply.kz/sandbox/healthz
```

Выполнять последовательно: при ошибке не переходить к restart. `-check` не открывает
базу или порт. Миграции применяются при успешном запуске нового backend.
Не заменять существующий `.env` примером из Git. Старый helper
`configure-billing-sandbox-env.py` выключает основной BCC checkout: он предназначен
для тестового контура, а не настройки боевого эквайринга.

## Параметры BCC

В существующем серверном `.env` заменить каждое присваивание один раз. Заполнить
семь `BCC_PRODUCTION_*` реальными значениями; до настройки callback оставить
оба переключателя checkout в `false`:

```dotenv
APP_ENV=production
PUBLIC_BASE_URL=https://ai-reply.kz
PAYMENT_MODE=off
BCC_CHECKOUT_ENVIRONMENT=production
BCC_CHECKOUT_ENABLED=false
BCC_CHECKOUT_PRODUCTION_APPROVED=false
BCC_RECURRING_ENABLED=false
BCC_PRODUCTION_MERCHANT_ID=
BCC_PRODUCTION_MERCHANT_NAME=
BCC_PRODUCTION_TERMINAL_ID=
BCC_PRODUCTION_MAC_KEY=
BCC_PRODUCTION_NOTIFY_USERNAME=
BCC_PRODUCTION_NOTIFY_PASSWORD=
BCC_PRODUCTION_STATE_SECRET=
```

`PAYMENT_MODE=off` относится к старому платёжному провайдеру; BCC имеет отдельные
переключатели. Соответствие реквизитов банка настройкам сервера:

| Что нужно | Переменная |
| --- | --- |
| Боевой merchantID от BCC | `BCC_PRODUCTION_MERCHANT_ID` |
| Название мерчанта от BCC | `BCC_PRODUCTION_MERCHANT_NAME` |
| Боевой terminalID от BCC | `BCC_PRODUCTION_TERMINAL_ID` |
| Результат побайтового XOR hex-компонент key1 и key2 | `BCC_PRODUCTION_MAC_KEY` |
| Отдельный согласованный с банком логин callback | `BCC_PRODUCTION_NOTIFY_USERNAME` |
| Новый случайный пароль callback, от 32 символов | `BCC_PRODUCTION_NOTIFY_PASSWORD` |
| Другой случайный серверный секрет, от 32 символов | `BCC_PRODUCTION_STATE_SECRET` |

`key1` и `key2` по отдельности backend не читает. Вычислять итоговый MAC локально,
не передавая компоненты сторонним сайтам. Пароль callback и state secret можно
создать двумя отдельными запусками `openssl rand -hex 32`. Значения production
должны отличаться от sandbox; state secret банку не передаётся.

На стороне банка настроить POST form-urlencoded, TLS 1.2 и тот же Basic Auth:
`https://ai-reply.kz:443/api/v1/payments/bcc/notify`.
Сначала проверить sandbox: оплату, итоговый notify, одно начисление при повторной
доставке и возврат. Затем можно включить оба переключателя основного checkout,
пройти `-check` и обновить контейнер. BACKREF не является подтверждением оплаты.
Base/Pro доступны отдельно через `BCC_RECURRING_ENABLED=true`, для изолированного
тестового контура — `BILLING_SANDBOX_BCC_RECURRING_ENABLED=true`. Пока банк не
подтвердил рекуррентные операции и их итоговый callback, оба оставить `false`.
Первый платёж требует отдельного согласия на продление. Каждый повторный платёж
фиксируется до отправки банку; неопределённый результат не запускает повторное
списание. Период доступа продлевает только подтверждённый callback. Отключение
продления в кабинете сохраняет уже оплаченный доступ.

`BCC_TEST_NOTIFY_URL` в `.env` должен быть обычным URL без Markdown-скобок.
Приёмник `/api/v1/payments/bcc/sandbox/notify` служит для диагностики банка;
начисление в изолированном контуре использует другой URL:
`https://ai-reply.kz:443/sandbox/api/v1/payments/bcc/notify`.

Для production-покупок App Store используются `APPLE_IAP_ENABLED=true`,
`APPLE_IAP_BUNDLE_ID=kz.ai-reply.reply.keyboard.keyboard`,
`APPLE_IAP_ACCEPT_SANDBOX=false`. До согласования цены продажи в админке остаются
выключенными. Сервер проверяет подписанные StoreKit-транзакции встроенным
сертификатом Apple; банковские key1/key2 к ним не относятся.

## Выпуск Mac

Developer ID Application команды `2PK6339Q47` выпущен и установлен 1 ноября 2026,
действует до 1 декабря 2027. Закрытый ключ находится в приватном каталоге
`~/Library/Application Support/AIReply/signing/`. Ключ и пароль не добавлять в Git.

В обычном Терминале Mac один раз сохранить доступ к нотариализации. Пароль
приложения Apple вводится только в интерактивный запрос:

```sh
xcrun notarytool store-credentials airreply-notary --apple-id nongarov7@gmail.com --team-id 2PK6339Q47
```

Из `AirReply-macOS` выполнить `scripts/build-release.sh`. Скрипт проверяет команду
сертификата, Intel + Apple Silicon, production API, принятие Apple и прикреплённые
notarization tickets. Готовые файлы: `dist/AI-Reply-1.0.0.dmg`, соответствующий
`.dmg.sha256` и `dist/macos-1.0.0.json`. Промежуточный архив не заменяет готовый DMG.

## Публикация скачивания

Создать на сервере `/srv/ai-reply/downloads/macos/`, загрузить туда три готовых
файла. Каталог содержит только публичные релизы; ключи и `.env` находятся отдельно.
Каталоги должны читаться пользователем Caddy, файлы иметь права 644.

В существующем HTTPS-блоке Caddy добавить, сохранив текущие TLS и reverse proxy:

```caddyfile
handle_path /downloads/macos/* {
    root * /srv/ai-reply/downloads/macos
    header X-Content-Type-Options nosniff
    file_server
}
```

При использовании `handle` для proxy оставить его fallback-обработчиком. Проверить
checksum на сервере, конфигурацию Caddy перед reload и публичную доступность DMG:

```sh
cd /srv/ai-reply/downloads/macos
sha256sum -c AI-Reply-1.0.0.dmg.sha256
caddy validate --config /etc/caddy/Caddyfile
systemctl reload caddy
curl --fail --head https://ai-reply.kz/downloads/macos/AI-Reply-1.0.0.dmg
```

После проверки задать в серверном `.env` и пересоздать backend:

```dotenv
MACOS_DOWNLOAD_URL=https://ai-reply.kz/downloads/macos/AI-Reply-1.0.0.dmg
```

Главная ведёт на `/mac` с инструкцией установки. Пока переменная пуста, сайт
сообщает, что установщик готовится. «Проверить обновления» в Mac открывает `/mac`.

## Общий баланс

Mac, iOS и сайт используют один серверный user ID. Проверенный банковский платёж
появляется в `/api/v1/me` уже открытой сессии без повторного входа. iOS обновляет
тариф и баланс при возвращении приложения на экран и при открытии тарифов.
Пакет пополняет ответы, а Base/Pro меняет тариф. В Mac баланс
обновляется при открытии строки ввода, активации приложения и кнопкой «Обновить
баланс» в настройках. Другой email может означать другой аккаунт, особенно при
Apple Hide My Email. Тариф и остаток общие; фактические цены зависят от канала.
Windows-приложения в репозитории пока нет, только архитектура и общий API.
