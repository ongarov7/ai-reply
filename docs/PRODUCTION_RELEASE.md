# Выпуск из main: backend, сайт и Mac

На Linux работают API, личный кабинет, оплата и выдача DMG. Приложение работает
на Mac пользователя. Подпись и нотариализация выполняются на Mac; закрытый ключ
на сервер не переносится. Секретные значения владелец заполняет в серверном `.env`.

## Утверждённые условия нового выпуска — 9 ноября 2026

Владелец подтвердил следующие целевые цены для Казахстана. Уточнение для
Pro Monthly: **3 990 ₸ в месяц**, а не 3 999 ₸.

| Предложение | Что получает покупатель | Целевая цена |
| --- | --- | --- |
| Base, месяц | 1 000 ответов за каждый оплаченный месяц; неиспользованные ответы накапливаются без срока действия | 990 ₸/месяц |
| Pro, месяц | Безлимитные ответы на оплаченный период | 3 990 ₸/месяц |
| Pro, год | Безлимитные ответы на оплаченный период | 39 990 ₸/год |
| Разовый пакет | 30 ответов без срока действия | 130 ₸ |

Base начисляется за оплаченный период подписки: это не обнуляемая календарная
месячная квота. Отмена продления или окончание подписки прекращает будущие
начисления, но сохраняет накопленный остаток. Годовой Base не включён в новую
утверждённую линейку; новая цена для него не задана.

В App Store Connect нужно сначала проверить точные ценовые категории для
Kazakhstan, включая See Additional Prices. Если целевой цены нет, владелец
разрешил выбрать ближайшую доступную. После сохранения у Apple нужно перенести
фактически выбранную цену в серверный каталог: iOS сравнивает его с StoreKit и
блокирует покупку при расхождении. Инструкция Apple:
https://developer.apple.com/help/app-store-connect/manage-subscriptions/manage-pricing-for-auto-renewable-subscriptions/.

### Состояние проверки 9 ноября

Изменение цен в App Store Connect, публикация рабочего каталога и отправка на
App Review ещё не подтверждены. Apple приняла загрузку iOS 1.2.0 (24)
9 ноября в 02:19 Asia/Almaty (`Upload succeeded`; пакет перешёл в processing).
Экспорт разрешает последующую отправку на App Review; режима только внутреннего
тестирования в export options нет. Доступность сборки тестировщикам требует
отдельной проверки в TestFlight.

Публичный production API по-прежнему возвращает каталог версии 5: Base Monthly
790 ₸ и 40 ответов в день, Base Yearly 7 900 ₸, Pro Monthly 3 450 ₸,
Pro Yearly 34 900 ₸, разовый пакет 30 ответов за 130 ₸.

В рабочей копии подготовлена миграция `0037_base_monthly_permanent_bundle.sql`
и начисление постоянного пакета за каждый подтверждённый месяц Apple или BCC.
Количество в уже выданном пакете не меняется при повторной доставке транзакции
или изменении каталога. Окончание подписки и переход на Pro сохраняют остаток;
возврат снимает только неиспользованную часть, отмена возврата восстанавливает
снятый остаток. `monthly_limit` в товаре описывает начисляемый пакет; в usage
календарная месячная квота остаётся нулевой.

Момент применения миграции фиксируется в `apple_base_monthly_since`. Более
ранние Apple-покупки сохраняют прежние постоянные дневные начисления из 0036;
их повторная доставка не создаёт новый месячный пакет. Действующие BCC-мандаты
используют сохранённые условия заказа, прежние ручные назначения Base сохраняют
дневное условие. Годовой Base снимается с новых продаж; прежние покупки и остатки
сохраняются. `.env` и рабочие базы локальная подготовка не меняет.

iOS 1.2.0 (24) собрана с production API для приложения и клавиатуры. 69 тестов
покупок, подписок и баланса прошли на iPhone 17e / iOS 26.5 Simulator; Release
archive собран, строгая проверка подписи прошла. Проверка настоящей покупки в
Sandbox этими результатами не подтверждается. Артефакты:
`outputs/release-1.2.0-24/` (не включены в Git).

Перед отправкой на App Review сохранить реальные цены App Store, развернуть
backend с 0037, проверить совпадение цен и выполнить покупку/восстановление в
Sandbox. Владелец открыл SSH-соединение с сервером `154.59.111.195` через
`~/.ssh/ai-reply-release.sock`. Checkout обновлён до релизных изменений, запущена
сборка Docker-образа. Подтверждённой выкладки нового контейнера пока нет.

## Прежние единые тарифы — 2 ноября 2026

Сайт, desktop и App Store используют один бизнес-каталог в KZT:

| Предложение | Лимит | Цена | Apple product ID |
| --- | --- | --- | --- |
| Бесплатный | 10 приветственных ответов однократно | 0 ₸ | — |
| Base, месяц | 40 ответов в день | 790 ₸ | `kz.aireply.base.monthly` |
| Base, год | 40 ответов в день | 7 900 ₸ | `kz.aireply.base.yearly` |
| Pro, месяц | Безлимит | 3 450 ₸ | `kz.aireply.pro.monthly` |
| Pro, год | Безлимит | 34 900 ₸ | `kz.aireply.pro.yearly` |
| Дополнительный пакет | 30 ответов | 130 ₸ | `kz.aireply.pack30` |

Миграция `0029_unified_pricing.sql` вводит общую модель тарифов.
`0030_pro_yearly_store_price.sql` устанавливает выбранные владельцем 34 900 ₸/год
для Pro — доступный уровень App Store. Она создаёт новую публикацию только при
необходимости и сохраняет остальные правки существующего черновика, обновляя его
базовую версию и номер ревизии. Уже опубликованная через админку цена 34 900 ₸
не создаёт повторную публикацию. Старые заказы, суммы действующих банковских мандатов, история
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

## Накопление ответов Base в App Store — 8 ноября 2026

Для `kz.aireply.base.monthly` и `kz.aireply.base.yearly` 40 ответов в день
начисляются в постоянный баланс аккаунта. Неиспользованные ответы переносятся
на следующие дни и доступны после окончания подписки. Окончание оплаченного
периода прекращает новые начисления; накопленный остаток не обнуляется.
Баланс общий для всех устройств аккаунта. Pro остаётся безлимитным,
разовый пакет из 30 ответов также не имеет срока действия.

Это изменение относится к подпискам App Store. Дневные условия прежних
ручных и банковских подписок не меняются. Перед повторной отправкой iOS
нужно развернуть обновлённый backend с миграцией `0036_apple_base_rollover.sql`
и обновить описание Base в App Store Connect: ответы накапливаются и не имеют
срока действия. Один `git push` не развёртывает backend и не отправляет
сборку на проверку.

## Повторная проверка подписок — 4 ноября 2026

Apple отклонила 1.2.0 (17) по 2.2: приложение показывало платные тарифы
с сообщением о недоступности покупки. На странице Business 4 ноября подтверждены
статусы: платный договор — «Ожидание сведений о пользователе», обе налоговые
формы — «Не указана налоговая информация», банк — «Обработка». Это ещё не
активный платный договор. Apple требует действующий Paid Apps Agreement и для
покупок во время App Review:
https://developer.apple.com/help/app-review/before-submitting-for-review/configure-in-app-purchases/.
Налоговые формы и юридические подтверждения завершает владелец аккаунта.

Сборка 1.2.0 (18) убирает карточки тарифов без соответствующего товара StoreKit,
позволяет повторить загрузку каталога и отбрасывает устаревшие ответы после смены
аккаунта или выключения продаж. Это исправляет интерфейс, но не активирует
договор Apple и не доказывает работоспособность покупки.

Перед повторной отправкой:

- дождаться статуса **Active / Активно** у платного договора;
- проверить на реальном устройстве цены всех пяти товаров, покупку в Sandbox,
  подтверждение на сервере и фактическое начисление, восстановление подписки;
- использовать production API `https://ai-reply.kz` и тот же аккаунт, который
  указан для App Review. `APPLE_IAP_SANDBOX_ACCOUNTS` должен включать его email;
  пустой список использует `REVIEW_LOGIN_EMAIL`, явный список заменяет этот
  запасной вариант. Оставить `APPLE_IAP_ACCEPT_SANDBOX=false`;
- для тестовых покупок через production API направить **Sandbox Server URL**
  Apple на `https://ai-reply.kz/api/v1/iap/apple/notifications` (Version 2).
  Исправленный backend принимает подписанные Sandbox-события только для
  разрешённых аккаунтов. Отдельный `/sandbox` использует другую базу: смена
  единственного Sandbox URL прекращает доставку событий в этот отдельный
  контур. Нельзя считать оба контура проверенными одним URL;
- выбрать новую сборку для версии 1.2.0 и отправить её вместе с подписками
  после успешной проверки. Статус товара «Готово к проверке» не равен одобрению.

Публичная проверка 4 ноября: production `iap_ios=true`, каталог v4 содержит
все пять правильных Apple ID, тарифы активны и доступны для iOS. Это проверка
серверного каталога, а не ответ StoreKit и не подтверждённая оплата.

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

После настройки существующего `.env` блок ниже можно выполнить готовым скриптом:
`bash scripts/deploy-with-backup.sh` из каталога backend. Скрипт не меняет ветку
и не выполняет `git pull`; он проверяет конфигурацию, сохраняет приватную копию
`.env` и обеих баз, сохраняет прежний образ, пересоздаёт backend и проверяет
прежний том и публичные endpoints. Он рассчитан на уже работающий production
с включённым отдельным `/sandbox`.

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
`79000`, `790000`, `345000`, `3490000`, `13000` соответственно. Sandbox-проверки
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
