AI Reply for Windows — 1.0.0 preview

Для установки запустите AIReply-Setup.exe и следуйте шагам мастера.
Он сам выберет x64 или ARM64 и добавит AI Reply в меню «Пуск».
Установка выполняется для текущего пользователя без прав администратора.
Удаление: Параметры Windows → Приложения → AI Reply.
Для обновления сначала выйдите из AI Reply через меню в трее и запустите новый Setup.

Если вы скачали ZIP: распакуйте его в постоянную папку и запустите AIReply.exe.
win-x64 — компьютеры Intel/AMD; win-arm64 — Windows на ARM.
.NET включён: устанавливать его отдельно не нужно.

Войдите по тому же email, что в macOS/мобильном приложении.
Быстрый вызов: Alt+Space. При конфликте AI Reply попробует
Ctrl+Alt+Space, затем Ctrl+Alt+Shift+Space. Фактическое сочетание
показано в настройках. Значок в трее: открыть, настройки, выход.

Enter — отправить; Shift+Enter / Alt+Enter — новая строка.
Ctrl+Enter — копировать ответ; Ctrl+D — диктовка; Esc — отмена/скрыть.
Автоматическую вставку и автозапуск можно включить в настройках.
Оплата открывается на сайте. Баланс общий для того же аккаунта.

Сессия и локальные профили защищены Windows DPAPI текущего пользователя.
Настройки: %LOCALAPPDATA%\AI Reply\settings.json.
Выход из аккаунта оставляет его профили на этом компьютере;
удалить их можно в Настройках → Профили.

Сборка пока не подписана. Выпуск для всех пользователей требует
ручной проверки на Windows 10/11: микрофон, трей, горячие клавиши,
вставка в мессенджеры, масштабирование и несколько мониторов.

Run AIReply-Setup.exe to install for the current user. It selects x64/ARM64,
adds a Start menu entry and supports removal through Windows Settings.
Quit AI Reply from the tray before updating. User account data is preserved.
For portable ZIPs, unzip and run AIReply.exe. .NET is bundled.
Use the same email as on your other devices. This is an unsigned
preview pending Windows 10/11 manual QA, not a production release.
Support: https://ai-reply.kz/
