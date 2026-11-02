# Выпуск из main: backend, сайт и Mac

На Linux работают API, личный кабинет, оплата и выдача DMG. Приложение работает
на Mac пользователя. Подпись и нотариализация выполняются на Mac; закрытый ключ
на сервер не переносится. Секретные значения владелец заполняет в серверном `.env`.

## Единые тарифы — 2 ноября 2026

Сайт, desktop и App Store используют один бизнес-каталог в KZT:

| Предложение | Лимит | Цена | Apple product ID |
| --- | --- | --- | --- |
| Бесплатный | 10 приветственных ответов однократно | 0 ₸ | — |
| Base, месяц | 40 ответов в день | 790 ₸ | `kz.aireply.base.monthly` |
| Base, год | 40 ответов в день | 7 900 ₸ | `kz.aireply.base.yearly` |
| Pro, месяц | Безлимит | 3 450 ₸ | `kz.aireply.pro.monthly` |
| Pro, год | Безлимит | 34 500 ₸ | `kz.aireply.pro.yearly` |
| Дополнительный пакет | 30 ответов | 130 ₸ | `kz.aireply.pack30` |

Миграция `0029_unified_pricing.sql` обновляет существующую базу и публикует новую
версию каталога. Старые заказы, суммы действующих банковских мандатов, история
каталога и остатки приветственного бонуса сохраняются. Использованные ответы
не сбрасываются. Новый приветственный бонус повторно не начисляется.

Перед отправкой iOS на проверку сохранить эти цены для Kazakhstan в App Store
Connect, сверить периоды и привязать фактически настроенные товары в основном
каталоге `/admin/billing` и отдельно в `/sandbox/admin/billing`. Приложение показывает
фактическую цену StoreKit и блокирует покупку при расхождении с каталогом.
Миграция сохраняет текущие привязки и переключатели продаж; она не создаёт товары
Apple и не включает ненастроенные покупки.

Для публичной сборки app и keyboard должны использовать `https://ai-reply.kz`.
Экспорт для TestFlight и последующего App Review не должен содержать
`testFlightInternalTestingOnly=true`.

## Обновление сервера

Использовать существующий checkout, Compose project и том базы. Сначала сделать
приватную копию `.env`, затем отредактировать его по следующему разделу. При изменённом checkout
сначала сохранить правки; не применять `reset --hard` или `docker compose down -v`.

```sh
cd /home/ai-reply/ai-reply-back-end
umask 077
cp .env ".env.before-release-$(date -u +%Y%m%dT%H%M%SZ)"
cd ..
git status --short
git fetch origin
git switch main
git pull --ff-only origin main
cd ai-reply-back-end
```

После настройки `.env` выполнить весь блок в Bash. Сборка и проверка происходят
до остановки. На время копирования останавливается весь backend: обе базы
`aireply.db` и `aireply-sandbox.db` вместе с WAL/SHM находятся в `/app/data`.
Наличие `sqlite3` или Python внутри Alpine-контейнера не требуется.

```bash
(
set -Eeuo pipefail
cd /home/ai-reply/ai-reply-back-end
umask 077
aireply_stamp=$(date -u +%Y%m%dT%H%M%SZ)
aireply_backup="/home/ai-reply-backups/$aireply_stamp"
aireply_container=$(docker compose ps -q backend)
test -n "$aireply_container"
aireply_image=$(docker inspect --format '{{.Image}}' "$aireply_container")
aireply_volume=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{.Name}}{{end}}{{end}}' "$aireply_container")
test -n "$aireply_volume"
docker tag "$aireply_image" "ai-reply-backend:before-$aireply_stamp"
mkdir -p /home/ai-reply-backups
chmod 700 /home/ai-reply-backups
mkdir -m 700 "$aireply_backup" "$aireply_backup/data"
cp -p .env "$aireply_backup/.env"
chmod 600 "$aireply_backup/.env"
printf '%s\n' "$aireply_image" > "$aireply_backup/image-id"
printf '%s\n' "$aireply_volume" > "$aireply_backup/volume-name"
docker compose config --quiet
docker compose build backend
docker compose run --rm --no-deps backend -check
# This trap applies only before any new process can migrate the databases.
trap 'docker compose start backend >&2 || true' EXIT
docker compose stop -t 30 backend
docker cp "$aireply_container:/app/data/." "$aireply_backup/data/"
test -s "$aireply_backup/data/aireply.db"
test -s "$aireply_backup/data/aireply-sandbox.db"
# Backup succeeded. Disable automatic old-container restart before migrations.
trap - EXIT
docker compose up -d --no-deps --wait --wait-timeout 90 backend
aireply_new_container=$(docker compose ps -q backend)
test "$aireply_volume" = "$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{.Name}}{{end}}{{end}}' "$aireply_new_container")"
docker compose ps backend
curl --fail --silent --show-error --max-time 15 https://ai-reply.kz/healthz
curl --fail --silent --show-error --max-time 15 https://ai-reply.kz/sandbox/healthz
curl --fail --silent --show-error --max-time 15 https://ai-reply.kz/api/v1/billing/catalog
curl --fail --silent --show-error --max-time 15 https://ai-reply.kz/sandbox/api/v1/billing/catalog
printf '\nBackup: %s\n' "$aireply_backup"
)
```

При ошибке до копирования новый backend не запускается; при ошибке остановки или
бэкапа trap запускает прежний контейнер. После начала `compose up` автоматического
отката нет: новая версия уже могла применить миграции. Для отката сначала остановить
backend и восстановить **весь** сохранённый каталог данных и соответствующий образ,
а не запускать старый образ поверх изменённой базы.

`-check` не открывает базу или порт и не проверяет доставку писем или сообщений Telegram.
Миграции применяются при запуске нового backend. Оба health URL должны ответить
успешно; оба каталога должны вернуть лимит Base 40 и цены в минимальных единицах:
`79000`, `790000`, `345000`, `3450000`, `13000` соответственно. Sandbox-проверки
предполагают уже включённый `BILLING_SANDBOX_ENABLED=true`.
Не заменять существующий `.env` примером из Git. Старый helper
`configure-billing-sandbox-env.py` выключает основной BCC checkout: он предназначен
для тестового контура, а не настройки боевого эквайринга.

## Исправления существующего .env

Для каждого ключа ниже оставить ровно одно присваивание: удалить прежние
противоречащие строки, не дописывать второй набор в конец. Сохранить текущие
`APP_PORT`, секреты сессий, OAuth, Firebase и остальные действующие параметры.
Значения в угловых скобках заменить приватно на сервере до запуска `-check`.

```dotenv
APP_ENV=production
PUBLIC_BASE_URL=https://ai-reply.kz
AUTH_DEMO_MODE=false
LEGACY_API_ENABLED=false
PAYMENT_MODE=off
ADMIN_SECURE_COOKIES=true
TRUST_PROXY=true
APPLE_IAP_ENABLED=true
APPLE_IAP_BUNDLE_ID=kz.ai-reply.reply.keyboard.keyboard
APPLE_IAP_ACCEPT_SANDBOX=false
RESEND_API_KEY=<существующий ключ Resend>
RESEND_FROM_EMAIL=noreply@ai-reply.kz
RESEND_FROM_NAME=AI Reply
TELEGRAM_ALERTS_ENABLED=true
TELEGRAM_BOT_TOKEN=<токен вашего бота>
TELEGRAM_CHAT_ID=-1004298283794
```

Resend должен разрешать отправку с указанного адреса на нужных получателей;
тестовый ключ сам по себе этого не подтверждает. После выпуска отдельно проверить
получение письма входа на реальном адресе и сообщение в Telegram через
`docker compose exec backend /app/server -alert-test`. Бот должен иметь право писать
в указанный канал. Не выводить `.env`, ключи и токены в чат или журнал развёртывания.

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
