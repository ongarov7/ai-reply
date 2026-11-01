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
BCC_PRODUCTION_MERCHANT_ID=
BCC_PRODUCTION_MERCHANT_NAME=
BCC_PRODUCTION_TERMINAL_ID=
BCC_PRODUCTION_MAC_KEY=
BCC_PRODUCTION_NOTIFY_USERNAME=
BCC_PRODUCTION_NOTIFY_PASSWORD=
BCC_PRODUCTION_STATE_SECRET=
```

`PAYMENT_MODE=off` относится к старому платёжному провайдеру; BCC имеет отдельные
переключатели. Боевой MAC — XOR компонент банка: вычислять локально, не передавая
компоненты сторонним сайтам. Notification password и state secret — разные
случайные секреты не короче 32 символов.

На стороне банка настроить POST form-urlencoded, TLS 1.2 и тот же Basic Auth:
`https://ai-reply.kz:443/api/v1/payments/bcc/notify`.
Сначала проверить sandbox: оплату, итоговый notify, одно начисление при повторной
доставке и возврат. Затем можно включить оба переключателя основного checkout,
пройти `-check` и обновить контейнер. BACKREF не является подтверждением оплаты.
Исходная Payment реализует BCC-пакет; MIT-автопродление Base/Pro требует отдельной
реализации и проверки, наличие подписи `TRTYPE=171` само его не включает.

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

Mac и сайт используют один серверный user ID. Проверенный банковский платёж
появляется в `/api/v1/me` уже открытой Mac-сессии без повторного входа. Баланс
обновляется при открытии строки ввода, активации приложения и кнопкой «Обновить
баланс» в настройках. Другой email может означать другой аккаунт, особенно при
Apple Hide My Email. Тариф и остаток общие; фактические цены зависят от канала.
Windows-приложения в репозитории пока нет, только архитектура и общий API.
