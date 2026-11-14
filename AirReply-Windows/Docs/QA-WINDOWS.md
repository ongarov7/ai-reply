# Windows 10/11 manual release gate

Status: not executed on a physical Windows desktop in this macOS development session.
GitHub Actions native tests validate selected Win32 calls on Windows x64; they cannot establish
messenger compatibility, microphone privacy UX, visual fidelity or ARM64 runtime behavior.

Record OS/build, architecture, app commit, DPI, monitor setup, target app/version and outcome for each case.

- [ ] Installer: download through a browser, run unsigned Setup on a standard Windows user, icon/publisher,
  install/uninstall UI, optional desktop shortcut, reinstall and running-app message; real ARM64 selection.
- [ ] Clean install: first interactive launch opens sign-in; email OTP, invalid/expired code, resend/cooldown,
  same account as macOS, consent accept/failure, shared balance; restart restores the session.
- [ ] Generation: compose, manual reply, fresh clipboard reply, four recipients, limits/unicode, cancellation,
  retry reuses the request, regenerate makes a new request, refunded output failure, offline/reconnect.
- [ ] Plans: browser handoff selects the expected account; foreground return and delayed bank notification
  update the visible balance within the bounded catch-up window; failures never open another browser account.
- [ ] Tray/single instance: launch twice; autostart `--background` stays hidden; tray Open/Dictate/Settings/Quit;
  no taskbar bar-window; actual fallback gesture displayed and saved gesture retained after restart.
- [ ] Hotkeys: all three startup candidates occupied; custom/reserved combinations; shortcut capture cancelled
  by Escape, losing focus and closing Settings; keyboard layout changed while the bar is open.
- [ ] Clipboard: password manager concealed data, non-text, stale text, own copy, clipboard busy/retry;
  large source visible without being sent; repeated copy yields only one paste.
- [ ] Focus/paste: restore before hiding; Telegram/WhatsApp desktop, browser input, Notepad; Ctrl held during
  copy, user switches target during delay, target closes, elevated target produces copy-only, RU/KK/EN layouts.
- [ ] Voice: mic disabled in Settings, no device, 0.4 s recording, normal/40 s recording, default-device change,
  Bluetooth startup, unplug, lock/suspend during acquisition and capture, cancel upload, offline upload;
  local WAV is removed after completion/cancel. Inspect temp directory after normal Quit and force kill.
- [ ] Context: capability absent hides controls; per-account profiles, metadata edits while document imports,
  unsupported/oversized/protected/malformed document errors; imports never generate; unsaved/delete confirmations;
  instructions and extended length reset after Copy/new conversation; private state clears on sign-out/switch.
- [ ] Visuals: light/dark/system/high contrast and transparency disabled, long Kazakh strings, keyboard navigation,
  screen reader names; Windows accent; DPI 100/125/150/200%, negative monitor origin, taskbar edges, monitor removal,
  drag across monitor/midline, grow result upward/downward, reopen at dragged position.
- [ ] ARM64: run the actual `win-arm64` ZIP on Windows ARM hardware; repeat native integration scenarios.
- [ ] Signing/distribution: preview EXE is available through the public download URL; verify browser reputation
  warnings. A signing certificate, dedicated download page and completion of this manual gate remain open.

Known limits: lexical retrieval does not translate synonyms or languages; scanned PDFs need a text layer;
200,000-segment retrieval performance is not benchmarked and there is no LRU index cache. PDF parsing is
in-process with cooperative cancellation between pages. DPAPI is bound to this Windows user; copied profile
files are not a portable backup. Kazakh additions require native-speaker review. Managed crash reports omit
messages, stack traces, source text and credentials; native Windows event-log crash import is not implemented.
