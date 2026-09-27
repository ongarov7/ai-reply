# AI Reply — передача работы (рефакторинг клавиатуры, 27.09.2026)

Этот файл — всё, что нужно, чтобы продолжить работу в новой сессии без потери контекста.
В конце есть **готовый промпт**: его можно просто скопировать.

---

## 1. Цель (исходное ТЗ, коротко)

Продукт: **AI Reply** — iOS-приложение + кастомная клавиатура (extension) + Go-бэкенд с админкой + Android-порт.
Задача: глубокий UX/UI и функциональный рефакторинг. Не редизайн вслепую: сначала аудит, потом исправления
с минимальными, поддерживаемыми изменениями.

Ключевые требования:

1. **Раскладки ҚАЗ / РУС / ENG — как у нативной клавиатуры Apple** (проверять, а не угадывать): цифры, символы,
   пунктуация, Shift / Caps Lock / Delete / Enter / пробел. Работают оба способа переключения: внутренняя кнопка
   ҚАЗ/РУС/ENG и системный глобус (`needsInputModeSwitchKey`, `handleInputModeList`).
2. **Крупные, удобные клавиши** без «мёртвых зон», адаптивно под все iPhone, светлая и тёмная тема в стиле iOS,
   без раскладок, захардкоженных под одно устройство.
3. **Компактная панель персон** (Дос / Клиент / Бизнес / Жұмыс / +): выбранное состояние, запоминание последней
   персоны, локализация; «+» явно значит «ещё / создать».
4. **Компактная AI-панель** (скрин №2 из ТЗ): рабочие Назад / Заново / Изменить / Вставить / Закрыть.
   «Изменить» даёт править любую часть текста, правки не теряются, «Вставить» вставляет именно отредактированный
   текст, «Заново» не затирает правки, сохраняет контекст и персону, показывает загрузку, не шлёт дубли, обрабатывает
   ошибки. Вставка надёжна для Unicode, казахских букв, эмодзи и многострочного текста. Клавиши работают в AI-режиме.
5. **Лимит сообщения 300 → 400**, источник правды — бэкенд/админка, клиентский fallback 400, проверка на сервере,
   ненавязчивый счётчик «238 / 400». Символы, токены и квоты — раздельные вещи.
6. Прогрессивный **онбординг** (включить клавиатуру, Full Access, глобус, копирование сообщения, персона,
   инструкция, generate / edit / regenerate / insert, раскрытие данных, актуальный путь в Настройках iOS),
   открывается повторно из Настроек / Помощи.
7. Необязательная короткая **анкета персонализации**, которая реально подключена к AI-пайплайну как
   структурированный контекст (не путать с персонами).
8. Локализация RU / KK / EN без смешения языков; ввод никогда не ждёт сеть; нажатие, хаптика, повтор delete,
   долгое нажатие; разные host-приложения и типы полей; только App Store-safe решения; явная state machine;
   сохранение черновиков; тесты; фазовая работа; финальный отчёт из 11 разделов.
9. **То же самое на Android** (эталон — Gboard).
10. Не объявлять задачу готовой, пока клавиатурой нельзя удобно печатать по-казахски и пока весь поток
    Generate → Edit → Regenerate → Insert не проверен.

Правила репозитория: коммиты без AI-подписей (никаких `Co-Authored-By` и т.п.). Не трогать bundle id, подпись,
App Group (`group.kz.yerek.replykeyboard`) и конфиг расширения без понимания последствий.
Не читать и не выводить `ai-reply-back-end/.env`.

---

## 2. Что сделано

### 2.1 Бэкенд — ГОТОВО, в `main` (все Go-тесты зелёные)

Лимиты AI теперь управляются из админки без релиза приложения.

- `internal/limits/` — сервис лимитов: **админ (БД) > env > код**, кэш 5 с, диапазоны
  (source 50–2000, instruction 50–1000, output tokens 32–1024), `FieldError` оборачивает `ErrInvalidRequest`.
  Ключи в `system_settings`: `max_source_characters`, `max_instruction_length`, `max_output_tokens`.
- `migrations/0004_ai_limits.sql` — ставит `max_source_characters = 400` (`INSERT OR IGNORE`), поэтому прод
  получит 400 сразу после деплоя, даже если в `.env` ещё 300.
- `config`: `LIMIT_SOURCE_TEXT_CHARS` по умолчанию 400 (`.env.example` обновлён).
- `internal/ai`: лимиты читаются на каждый запрос; `ErrSourceTooLong` с реальным лимитом; `max_output_tokens`
  из админки уходит провайдеру; `Profile.ReplyLanguage` (только проверенные коды kk/ru/en/uz, идёт в developer
  message, а не в user-блок — защита от prompt injection).
- API:
  - `GET /api/v1/config` отдаёт `max_source_characters`, `max_instruction_length` и `features.reply_preferences = true`.
  - Ответ на слишком длинное сообщение: `400 INVALID_REQUEST` + `details {field: "source_text", max_characters: N}`
    (старые клиенты видят тот же код).
  - `profile.reply_language` в `POST /api/v1/ai/reply`. Сервер декодирует с `DisallowUnknownFields`, поэтому
    клиент шлёт это поле, только если в config есть `features.reply_preferences`.
- Админка: `POST /api/v1/admin/settings/limits` (CSRF, аудит `settings.ai_limits.update` со значениями
  до/после, валидация по полям); в `GET /admin/settings` есть блок `ai_limits` (текущие значения, дефолты,
  что переопределено, диапазоны); карточка «AI limits» в Settings (`admin-app.js`), переводы en/ru/kk/uz.
- Тесты: `internal/limits/limits_test.go` (8), `internal/apptest/limits_test.go` (5 e2e: 400 принимается,
  401 отклоняется с деталями, админ меняет лимит без релиза, валидация + CSRF, reply_language не утекает
  в user-часть промпта).

**Для деплоя:** применить миграцию 0004 (идёт автоматически при старте). По желанию выставить в проде
`LIMIT_SOURCE_TEXT_CHARS=400` (это только fallback, значение из админки главнее).

### 2.2 iOS — В РАБОТЕ, ветка `wip/ios-keyboard-refactor` (ещё НЕ собирается)

Аудит, найденные корневые причины:

- раскладки были придуманы, а не взяты у iOS: 12 колонок, `ъ`/`ё` на лице клавиатуры, растянутый казахский ряд;
- UIButton в UIStackView дают «мёртвые зоны» в зазорах и по краям коротких рядов;
- высота прыгает между 5-рядной казахской раскладкой и 4-рядными 123 / #+=;
- «Изменить» зависит от `becomeFirstResponder` внутри extension (ненадёжно), в режиме результата клавиши
  выбрасывались;
- «Заново» перезаписывает отредактированный черновик;
- персонализация (профиль) не уходит на сервер: серверный профиль пишется только при регистрации;
- лимит 300 захардкожен в нескольких местах; квота и rate limit показывались одной и той же ошибкой;
- «+» в панели персон только показывает тост; `¥` вместо `•` в символах.

Факты о нативной клавиатуре iOS 18.2 (проверено в Simulator):

- **KK**: верхний ряд `ә і ң ғ ү ұ қ ө һ` (9 клавиш по центру сетки из 11 колонок, того же размера, что остальные);
  дальше `й ц у к е н г ш щ з х` / `ф ы в а п р о л д ж э` / `⇧ я ч с м и т ь б ю ⌫` (⇧ и ⌫ шириной в одну клавишу).
- `ё` и `ъ` — долгим нажатием на `е` и `ь`.
- Цифры: `1–0` / `- / : ; ( ) ₽ & @ "` / `#+= . , ? ! ' ⌫`.
- Символы: `[ ] { } # % ^ * + =` / `_ \ | ~ < > $ € £ •`.
- Кнопка букв: `АӘБ` (RU `АБВ`, EN `ABC`). Return для KK — иконка ⏎ («Қайтару»), RU «Ввод».
  Пробел: «бос орын» / «Пробел» / «space».
- Высота клавиатуры **одинаковая** на 5-рядной и 4-рядной страницах.
- Светлая тема: фон `#D1D2D7`, буквы `#FFFFFF`, спец-клавиши `#ABAFBB`.
- `₸` мы добавили первым вариантом в долгое нажатие на любой клавише валюты.

Написано (файлы на ветке):

| Файл | Что это |
|---|---|
| `Shared/Keyboard/KeyboardLayout.swift` | Модель (`KeyAction`, `KeySlot`, `KeyRow`, `KeyboardPageSpec`), раскладки EN/RU/KK, цифры/символы, нижний ряд (123, глобус, ҚАЗ/РУС/ENG, `@ .` для email, `/ .` для URL, `@ #` для social), альтернативы долгого нажатия, `KeyboardLabels` (подписи пробела/return/кнопок, a11y) |
| `Shared/Keyboard/KeyboardGeometry.swift` | `KeyboardSizing` (размеры по ширине, landscape), постоянная высота (`keyAreaHeight(maximumRows:)`), раскладка в кадры + **hitFrame, покрывающие всю площадь** (без мёртвых зон), `keyIndex(at:)` |
| `Shared/Keyboard/KeyboardTyping.swift` | `ShiftState` (один раз / caps по двойному тапу / авто без перебивания ручного), `AutoCapitalization`, двойной пробел → «. », `TextDeletion.wordLength` |
| `Shared/AI/AILimits.swift` | Лимиты с сервера: кэш в App Group + память, fallback 400, самокоррекция по ответу сервера, флаг `features.reply_preferences`, `AILimitsRefresher` (клавиатура сама обновляет config раз в 6 ч при Full Access) |
| `Shared/AI/ReplyDraftHistory.swift` | Версии ответа: «Заново» ДОБАВЛЯЕТ версию, правки сохраняются, навигация ‹ 1/2 › |
| `Shared/AI/ReplyComposerFlow.swift` | **Явная state machine** композера: composing → generating(from) → result ⇄ editing → conflict; один запрос за раз, ошибка возвращает туда, откуда начали, Insert берёт текст с экрана |
| `ReplyKeyboard/KeyboardKeysView.swift` | Новая поверхность клавиш: один view на все касания, буква на отпускании + пузырь, slide-to-correct, rollover второго пальца, альтернативы, delete с повтором → по словам, трекпад по долгому пробелу, пикер языка по долгому нажатию, форвардинг глобуса, VoiceOver-элементы, клик и хаптика |
| `ReplyKeyboard/ComposerTextView.swift` | Поле, которое редактирует клавиатура **без first responder**: свой каретка, тап ставит каретку (TextKit 1), вставка/удаление/слово/сдвиг каретки |
| `ReplyKeyboard/ReplyComposerView.swift` | Переписан: компактный (≈120 pt в режиме ввода, 140–175 с ответом), рендерит `ReplyComposerFlow`, счётчик «N / лимит» в шапке |
| `ReplyKeyboard/ReplyContext.swift` | Координатор на `ReplyComposerFlow`: генерация с «тикетом» (поздний ответ не всплывает), stop, back, edit, версии, insert/conflict, **парковка сессии на 10 мин** в памяти при скрытии клавиатуры |
| `Shared/AI/AIReplyError.swift` | `+ quotaExhausted` (квота ≠ rate limit) |
| `Shared/Account/APIClient.swift` | `+ APIError.sourceTooLong(limit:)` из `details` |
| `Shared/Account/AccountReplyTransport.swift` | Шлёт **локальный профиль** (description/role/tone/business, `reply_language` — только если сервер поддерживает), маппинг квота/лимит/длина, запоминает реальный лимит |
| `Shared/Account/AccountModels.swift` | `ServerConfig.features` |
| `Shared/AI/AIConfiguration.swift`, `AIReplyService.swift`, `AIReplyStrings.swift`, `ReplyPromptBuilder.swift` | Лимит из `AILimits`, строки с `%d`, новые строки (stop, done, версии, счётчик), лимит инструкции от серверного |
| `Shared/Model/KeyboardLanguage.swift`, `SharedSettings.swift`, `UserProfile.swift` | Порядок ҚАЗ→РУС→ENG, включённые раскладки, последняя персона, хаптика, `replyLanguage` в профиле |
| `AIReply/Features/Account/AccountModel.swift`, `Compose/ComposeView.swift` | Приложение сохраняет лимиты из config; текст ошибки с лимитом |

---

## 3. Что осталось (по порядку)

**Checkpoint 1 — клавиатура iOS (ветка `wip/ios-keyboard-refactor`):**

1. **Переписать `ReplyKeyboard/KeyboardViewController.swift`** под новые компоненты:
   - заменить кэш страниц / `KeyButton` / `KeyboardMetrics` на `KeyboardKeysView` +
     `KeyboardGeometry.layout(page:sizing:areaHeight:)`;
   - `areaHeight = sizing.keyAreaHeight(maximumRows: KeyboardLayout.maximumRowCount(languages: enabled))`,
     одинаковая для всех страниц;
   - `options.showsNextKeyboardKey = needsInputModeSwitchKey` (проверять в `viewWillAppear` / `viewWillLayoutSubviews`,
     не в `viewDidLoad`); `showsLanguageKey = enabled.count > 1`; `field = KeyboardFieldKind(proxy.keyboardType)`;
     цифровые типы полей стартуют на плоскости numbers;
   - глобус: `handleInputModeList(from:with:)` из делегата; VoiceOver — `advanceToNextInputMode()`;
   - Shift через `ShiftState` + `AutoCapitalization.shouldCapitalize(before:type:)`;
     `KeyboardKeysView.setShiftMode`;
   - ввод: host (`textDocumentProxy`) или поле композера. В стадии `.result` любая буква/delete сначала вызывает
     `coordinator.beginEditingForTyping()`. Во время генерации `keysView.isInputDimmed = true`;
   - delete `.word` → `TextDeletion.wordLength` по `documentContextBeforeInput`, затем N раз `deleteBackward()`;
   - трекпад → `proxy.adjustTextPosition(byCharacterOffset:)` или `composer.moveCaret(by:)`;
   - смена языка: `KeyboardLanguage.next(in: enabled)`, сохранить, на ~1.2 с показать `nativeName` на пробеле;
   - return: `KeyboardLabels.returnKey(for:)`, prominent/disabled (`enablesReturnKeyAutomatically`);
   - `keysView.headroom` = высота бара над клавишами, `hapticsEnabled = hasFullAccess && SharedSettings.keyboardHapticsEnabled`;
   - `viewWillDisappear` → `coordinator.park()`; `viewWillAppear` → `ReplySessionParking.take()` → `coordinator.restore`;
   - `viewWillAppear` → `AILimits.reload()`; `viewDidAppear` при Full Access → `AILimitsRefresher.refreshIfStale()`;
   - реализовать новый `ReplyFlowCoordinatorDelegate` (`didOpen`, `coordinatorDidChange`) и отрисовку
     `ReplyComposerView.render(Content)`; ошибки — через `AIReplyStrings.message(for:)`.
2. **`KeyboardActionBar.swift`** — пробросить новый делегат композера
   (`composerDidTapPersona / Stop / Edit / Reply / PreviousVersion / NextVersion / didEdit field` и т.д.).
   Идея: паблик-API `render`, `insertText`, `deleteBackward`, `deleteWordBackward`, `moveCaret`,
   `acceptsTextInput`, `textBeforeCursor`, `preferredHeight`.
3. **`TemplateBarView.swift`** — выбранная персона (`SharedSettings.lastTemplateID`, писать при выборе);
   «+» → `UIMenu`: скрытые персоны + подсказка «создать в приложении» (`addTemplateHint`).
4. **Удалить `KeyButton.swift` и `KeyboardStrings.swift`** (заменены на `KeyCapView`/`KeySymbolCache` в
   `KeyboardKeysView.swift` и `KeyboardLabels`); проверить `QuickActionRow.swift` (высота 30, pill 28).
5. **`AIReply.xcodeproj/project.pbxproj`** (ручной, не XcodeGen) — добавить файлы Python-скриптом:
   - `Shared/Keyboard/*.swift`, `Shared/AI/AILimits.swift`, `ReplyDraftHistory.swift`, `ReplyComposerFlow.swift`
     → в Sources **приложения** (`C9FB49EBFCA504D0B6E3D1D2`) **и клавиатуры** (`8C5EA483B27D0C6DD74EFE06`);
   - `ReplyKeyboard/KeyboardKeysView.swift`, `ComposerTextView.swift` → только клавиатура;
   - новые тесты → `4A8B1311180674D57B80BB19`;
   - убрать ссылки на удалённые файлы.
6. **DEBUG-мок ответов для симулятора:** аргумент запуска `-AIReplyMockReplies` → флаг в App Group →
   клавиатура в DEBUG создаёт `AIReplyService(transportOverride:)` с разными ответами (казахский + эмодзи +
   перенос строки). В `AIReplyService.generate` пропускать `isReady`, если задан `transportOverride`.
7. **Тесты** (XCTest, `@testable import AIReply`):
   - обновить `AIReplyServiceTests` (300 → лимит из `AILimits`; clamp инструкции =
     `ReplyInstruction.maximumCharacters(serverLimit:)`) и `AccountAPITests` (квота → `.quotaExhausted`,
     `message(for: .quotaExhausted)`, `sourceTooLong`);
   - новые: раскладки (ряды KK/RU/EN/цифры/символы равны нативным, альтернативы `е→ё`, `ь→ъ`), геометрия
     (hitFrames покрывают всю площадь без дыр и пересечений на ширинах 320/375/390/402/430/landscape;
     одинаковая высота всех страниц), `ShiftState` (tap / double-tap caps / авто / ручной не перебивается),
     `AutoCapitalization`, `SpaceShortcut`, `TextDeletion`, `ReplyDraftHistory`, `ReplyComposerFlow`
     (нет дублей, fail → origin, regenerate добавляет версию, конфликт, cancel, resume), `AILimits`
     (fallback 400, мусор не принимается, `storeSourceLimit`), `AccountReplyTransport.profileBlock`
     (без `reply_language` для старого сервера).
8. **Собрать и прогнать** через watcher (раздел 5), исправить ошибки компиляции.
9. **Проверить в симуляторе:** печать по-казахски (все 9 букв, ё/ъ, Shift/Caps, 123/#+=, delete с ускорением),
   переключение ҚАЗ/РУС/ENG и глобус, высота не прыгает; Generate → Edit (правка в середине текста) →
   Regenerate (версия 2, версия 1 с правкой цела) → Insert (вставлен отредактированный текст, Unicode/эмодзи/
   многострочный), конфликт Replace/Add/Cancel, ошибки (офлайн, квота, слишком длинно).
   Открытая проблема: в симуляторе вместо нашей клавиатуры иногда рисуется системный QWERTY с подписью
   «AI Reply» на пробеле — разобраться (логи: `needsInputModeSwitchKey was called before a connection was
   established`, падения нет).

**Checkpoint 2:**

10. Онбординг: прогрессивный, повторно открывается из Настроек/Помощи, актуальный путь в Настройках iOS.
11. Анкета персонализации (коротко, опционально, редактируется) → `UserProfile` (`replyLanguage` уже есть)
    → `AccountReplyTransport.Profile`.
12. Настройки: включённые раскладки (`KeyboardLanguageStore.setEnabledLanguages`), хаптика.
13. Проход по локализации RU/KK/EN/UZ (`Localizable.xcstrings` — сохранять через
    `json.dumps(d, ensure_ascii=False, indent=2, separators=(',', ' : '))`, тогда файл совпадает побайтно),
    производительность, доступность.

**Checkpoint 3 — Android** (`AI-Reply-Android`): аудит, те же раскладки (эталон Gboard KK), без мёртвых зон,
постоянная высота, лимиты с сервера, композер (версии/редактирование/вставка). Сборка — `.claude-android`.

**Финал:** отчёт из 11 разделов (что было, корневые причины, что изменено, файлы, тесты, ручной QA,
ограничения iOS, риски, деплой, рекомендации, что дальше).

---

## 4. Ключевые решения (почему так)

- **Один view на все касания + hitFrame-мозаика** — единственный надёжный способ убрать мёртвые зоны;
  UIButton-ряды этого не умеют.
- **Буква вводится на отпускании** (как в iOS): иначе невозможны долгое нажатие (ё/ъ) и slide-to-correct.
  Задержку компенсирует rollover (второй палец сразу фиксирует первую букву).
- **Постоянная высота**: все страницы всех включённых раскладок укладываются в высоту самой высокой (5 рядов,
  если включён казахский). У 4-рядных страниц клавиши выше — так делает и нативная KK-клавиатура.
- **Композер без first responder**: `ComposerTextView` с собственной кареткой. Работает одинаково в любом
  host-приложении.
- **Версии вместо перезаписи**: «Заново» никогда не уничтожает правки, поэтому диалог «вы уверены?» не нужен.
- **Сервер — источник правды для лимитов**; клиент только предупреждает заранее и сам корректируется по ответу
  сервера. Новые поля профиля шлются только серверу, который объявил `features.reply_preferences`.
- **Приватность**: сообщение, инструкция и версии живут только в памяти; парковка — 10 минут, без диска.

---

## 5. Как собирать и тестировать

Терминал и Xcode на Mac доступны агенту только для кликов, поэтому сборкой управляет watcher.
Пользователь запускает его сам:

```
cd ~/ai-reply && ./claude-build-watch.sh
```

Агент создаёт файл-запрос в `~/ai-reply` и ждёт маркер:

| Запрос | Что делает | Результат |
|---|---|---|
| `.build-request` | `xcodebuild build` для симулятора | `ios-build.log`, `.build-done` (число строк с `error:`) |
| `.claude-test` (первая строка — опционально `-only-testing` id) | тесты | `ios-test.log`, `.claude-test-done` |
| `.claude-run` (первая строка — аргументы запуска) | ad-hoc сборка, установка, запуск `kz.yerek.replykeyboard` | `ios-run.log`, `.claude-run-done` |
| `.claude-simlog` | логи симулятора и краш-репорты | `ios-simlog.txt` |
| `.claude-android` | `gradlew assembleDebug testDebugUnitTest` | `android-build.log` |
| `.claude-emu` | install / tap / swipe / text / key на эмуляторе | `android-shot.png` |

- DEBUG-хук приложения: `-AIReplyDebugScreen keyboard` открывает сразу поле ввода с клавиатурой
  (ещё: `setup`, `home`, `settings`, `profile`, `templates`).
- В симуляторе клики надёжнее через AX `element_index`, чем по координатам.
- Go-тесты бэкенда: `cd ai-reply-back-end && go test ./...`. В облачном контейнере агента нужны
  `GOPROXY=direct GOSUMDB=off`.

---

## 6. Промпт для продолжения (скопировать в новую сессию)

```
Продолжи рефакторинг AI Reply по файлу ~/ai-reply/HANDOFF.md — прочитай его целиком первым делом.

Репозиторий: ~/ai-reply (GitHub yereke99/ai-reply). Бэкенд (лимиты из админки, 400 по умолчанию) уже в main.
iOS-работа — в ветке wip/ios-keyboard-refactor, она ещё НЕ собирается.
Переключись на неё (git checkout wip/ios-keyboard-refactor) и доведи Checkpoint 1 из раздела 3, по пунктам:
перепиши KeyboardViewController под KeyboardKeysView/KeyboardGeometry/ShiftState, обнови KeyboardActionBar и
TemplateBarView, удали KeyButton.swift и KeyboardStrings.swift, добавь новые файлы в project.pbxproj
(Shared → приложение + клавиатура, ReplyKeyboard → клавиатура, тесты → test target), добавь DEBUG-мок ответов,
обнови и допиши тесты, собери и прогони через watcher (раздел 5), проверь в симуляторе печать по-казахски и
поток Generate → Edit → Regenerate → Insert.

Нативные раскладки iOS уже проверены (раздел 2.2) — не выдумывай их заново.
Не ломай needsInputModeSwitchKey/advanceToNextInputMode, bundle id, подпись и App Group.
Не читай ai-reply-back-end/.env.
Коммиты — без AI-подписей, коммить только когда я попрошу; в main сливать, только когда всё собирается и
тесты зелёные.
Потом Checkpoint 2 (онбординг, анкета персонализации, настройки раскладок, локализация) и
Checkpoint 3 (Android по эталону Gboard), в конце — отчёт из 11 разделов.
Не объявляй задачу готовой, пока клавиатурой нельзя удобно печатать по-казахски и пока поток
Generate → Edit → Regenerate → Insert не проверен вживую.
Отвечай по-русски, коротко.
```
