# AI Reply — интерактивті өнім симуляторы

`/simulator` — клиентке, инвесторға және ішкі командаға өнімді көрсетуге
арналған интерактивті бет. Бұл слайд емес және скриншот жинағы емес: AI жауабы
нақты бэкенд арқылы, нақты квотамен және нақты промптпен жасалады.

```
Браузердегі симулятор
        │  әкімші сессиясы (cookie) + CSRF
        ▼
/api/v1/simulator/*  ──►  users · subscriptions · plans · ai  ──►  OpenAI
        │                      (мобильді API-мен бірдей сервистер)
        └── бір ғана демо аккаунт: simulator@demo.aireply.local
```

## Кіру

Мекенжай: `https://ai-reply.kz/simulator`
Тіркелгі деректері — **әкімші панелімен бірдей** (`ADMIN_EMAIL` / `ADMIN_PASSWORD`).
Бөлек құпиясөз жоқ: кодта да, фронтендте де сақталған тіркелгі деректері жоқ.

| Айнымалы | Не үшін |
|---|---|
| `SIMULATOR_ENABLED` | симулятор беттері мен API-і тіркеле ме; әдепкі — тек `development`/`test`. Өшірулі болса `/simulator` және `/api/v1/simulator/*` — 404. Көпшілік беттерде сілтеме жоқ, мекенжай тек URL арқылы |
| `ADMIN_EMAIL` | симуляторға да, `/admin`-ге де кіру |
| `ADMIN_PASSWORD` | сол құпиясөз (PBKDF2-мен хэштеліп сақталады) |
| `ADMIN_SESSION_TTL` | сессия мерзімі (әдепкі 8 сағат) |
| `ADMIN_SECURE_COOKIES` | бос болса — `PUBLIC_BASE_URL` https кезінде `true` |
| `RATE_ADMIN_LOGIN_PER_HOUR` | кіру әрекеттерінің шегі IP бойынша (симулятормен ортақ); бір поштаға 15 минутта 5 әрекет |

Жаңа әкімші қосу қажет болса — `ADMIN_EMAIL`/`ADMIN_PASSWORD` арқылы bootstrap,
одан әрі әкімші панелі. Симулятор үшін жеке тіркелгі жасаудың қажеті жоқ.

## Не нақты, не демо

| Бөлік | Дереккөз |
|---|---|
| AI генерациясы | **нақты** — `ai.Service` → квота → OpenAI |
| Профиль және бизнес контексі | **нақты** — `users.Service.UpdateProfile` |
| Квота, күндік лимит, қайта жаңару | **нақты** — `subscriptions.Service.Entitlement` |
| Тариф каталогы | **нақты**, тек оқу — `plans.Service` |
| Жүйе күйі (модель, орта, белдеу) | **нақты** — конфигурациядан |
| Әкімші панеліндегі қолданушылар тізімі | демо — `internal/simulator/demodata.go` |
| Дашборд графиктері | демо — сол файлда |
| Баға өзгерту | демо қабат, жадта (`PlanDraft`), `plans` кестесіне жазылмайды |
| Тіркелу / SMS коды | интерфейс жүрісі; нақты OTP эндпоинті қозғалмайды |
| Сөзді тану | құрылғыда (браузердің Web Speech API), дәл Android қосымшасындағыдай |

Симулятордағы әр генерация **нақты** квотадан алынады және usage статистикасына
түседі — «шынымен жұмыс істейді» дегеннің мәні сол. Демо аккаунттың квотасын
көрсетілім алдында бүйір панельден нөлдеуге болады.

## Демо аккаунт

| | |
|---|---|
| ID | `00000000-0000-4000-8000-0000000000d1` |
| E-mail | `simulator@demo.aireply.local` |
| `kind` | `simulator` |

Алғаш кіргенде автоматты жасалады. Симулятор арқылы жасалған әр жазу тек осы
аккаунтқа тиеді — басқа қолданушының профилі де, жазылымы да өзгермейді
(`TestSimulatorProfileIsSavedThroughTheRealService` осыны тексереді).

## Эндпоинттер

Барлығы әкімші сессиясын талап етеді; күй өзгертетіндері — `X-CSRF-Token`.

| Метод | Жол | Не істейді |
|---|---|---|
| GET | `/api/v1/simulator/bootstrap` | сессия, аккаунт, тарифтер, жүйе күйі |
| GET | `/api/v1/simulator/health` | дерекқор + провайдер бапталған ба |
| GET | `/api/v1/simulator/account` | профиль, жазылым, квота |
| POST | `/api/v1/simulator/account/profile` | профильді сақтау (нақты) |
| POST | `/api/v1/simulator/account/plan` | демо аккаунтқа тариф беру |
| POST | `/api/v1/simulator/account/reset` | профиль + квота + тегін тариф |
| POST | `/api/v1/simulator/account/reset-quota` | тек бүгінгі квота |
| POST | `/api/v1/simulator/generate` | нақты AI жауабы (квота + OpenAI) |
| POST | `/api/v1/simulator/transcribe` | `501` — сервер жағында тану жоқ |
| GET | `/api/v1/simulator/admin/overview` | демо тізім + нақты каталог/күй |
| POST | `/api/v1/simulator/admin/plan-draft` | демо баға (жадта) |
| POST | `/api/v1/simulator/admin/plan-draft/reset` | демо қабатты тазалау |

Беттер: `GET /simulator/login`, `POST /simulator/login`, `POST /simulator/logout`,
`GET /simulator` және `GET /simulator/{path...}` (SPA).

## Қауіпсіздік

- OpenAI кілті тек серверде; симулятор беті де, JS те оны ешқашан көрмейді.
- Сессия — HttpOnly cookie, күй өзгертетін сұраныста CSRF тақырыбы міндетті.
- Сұраныс инспекторы ақ тізіммен жұмыс істейді: токен, тақырып, cookie, тіркелгі
  деректері ешқашан көрсетілмейді.
- `/simulator` тек өзіне `script-src 'unsafe-eval'` (Vue runtime компиляторы) және
  `microphone=(self)` алады; лендинг пен API қатаң саясатта қалады.
- Генерация жылдамдығы `RATE_AI_PER_MINUTE` бойынша, әкімші идентификаторымен
  шектеледі.
- Бет `noindex, nofollow`.
- Хабарлама мәтіні бұл жерде де ешқайда жазылмайды
  (`TestSimulatorStoresNoMessageText`).

## Өзгертулер

Интерфейс жолдары — `internal/localization/locales/*.json`, `sim.` префиксімен.
Төрт тілде де толық болуы керек: `TestSimulatorTranslationsAreCompleteInEveryLocale`
жетіспейтін кілт табылса сборканы құлатады.

Экрандар: `internal/transport/web/static/simulator/`

| Файл | Не үшін |
|---|---|
| `core.js` | күй, аударма, API клиенті, инспектор, ортақ компоненттер |
| `device.js` | iPhone/Android жақтауы, күй жолағы, сахна |
| `messenger.js` | телефон ішіндегі мессенджер |
| `keyboard.js` | AI Reply пернетақтасы |
| `voice.js` | дауыс абстракциясы және Android рұқсат терезесі |
| `phone.js` | толық сценарий (тіркелуден жіберуге дейін) |
| `views-product.js` | шолу, iOS, Android, пернетақта, дербестендіру |
| `views-explain.js` | сценарий, архитектура, экрандар галереясы |
| `views-admin.js` | әкімші демосы, тарифтер мен лимиттер |
| `app.js` | қабық, маршруттар, көрсетілім режимі, тур |

Құрастыру қадамы жоқ: әкімші панелі сияқты, браузерде тікелей жұмыс істейтін
Vue 3. Жаңа файл қосқанда оны `templates/simulator_app.gohtml` ішіне қосу керек.

## Тексеру

```bash
make test                       # барлығы, симулятор тесттерімен бірге
go test ./internal/apptest/ -run Simulator -v
```
