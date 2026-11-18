# Control Center icon correction — build 32

The user supplied a physical iPhone screenshot of build 31 with a blank AI Reply Voice control. The archive contains AIReplyControl as a Vector Glyph and cached images; missing asset packaging is ruled out. Precise remote rendering failure remains unproven. A fuller 27-variant symbol and explicit bundle references were investigated in a disposable app, but ScreenCaptureKit error -3811 blocked actual Control Center UI verification. Ordinary image previews are not evidence of Control Center rendering.

Build 32 uses the OS-owned `microphone.badge.ellipsis.fill` symbol through `Label(systemImage:)`, with `.contentTransition(.identity)`. UIKit probes on iOS Simulator 26.5 confirmed the glyph exists and rendered the literal microphone plus ellipsis badge. A runtime availability check selects the original `mic.fill` on older systems; it does not assume a guessed OS introduction version. Build 30's system-symbol path had previously rendered on the user's physical iPhone. The new compound glyph still requires physical Control Center confirmation through TestFlight.

Only the widget control label/symbol selection and four bundle build numbers/project.yml changed from build 31. Voice recording, keyboard, insertion/session binding, backend, entitlements and purchases are unchanged. No additional tests mirroring the icon implementation were introduced; archive compilation, signatures, App Intent metadata and source scope are checked for distribution. Build 31's App Review submission was withdrawn and observed as Removed before replacement.

Artifacts: outputs/release-1.3.0-32/qa/native-reply-microphone.png, scope-verification.json, archive/IPA verification and release-status.json. Disposable harness under qa/control-symbol-harness is not production source.

Update, later on 18 November: the blank control was reproduced in the simulator and fixed with a variable (v.3) custom symbol; see `2026-11-18-ios-voice-ui-polish.md`.
