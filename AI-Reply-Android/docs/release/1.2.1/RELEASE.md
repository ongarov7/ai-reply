# Android 1.2.1 (5) release handoff

Prepared on 13 November 2026. Package: `kz.yerek.aireply`.

## Release

Four distinct silent offline recordings in Kazakh, Russian, English and Uzbek demonstrate keyboard activation and selection, copied messages, optional microphone access, recording, actual reply generation and insertion. The resolved app language selects the video automatically; by default it follows the phone. A written guide remains available alongside the player.

The checked-in defaults are version name `1.2.1`, version code `5`. Play Console was checked: the highest uploaded code was `4` (`1.2.0`), in closed testing; production was inactive. Recheck before upload if another machine has uploaded a newer build.

The existing upload key is on the owner's other Mac. Use that key. No reset request was submitted and no Google Play signing key was changed. The existing upload certificate has SHA-256:

```text
55:D8:6D:5B:8C:5F:DE:F2:53:F5:B7:B2:A1:16:49:95:3D:25:8C:CD:93:4E:F1:DA:B9:97:88:72:70:F9:87:DC
```

## Build on the signing Mac

1. Pull `origin/main` and open `AI-Reply-Android` in Android Studio.
2. Run the checks:

   ```sh
   ./gradlew testDebugUnitTest lintRelease assembleDebug bundleRelease
   ```

3. Use **Build → Generate Signed Bundle / APK → Android App Bundle** and select the existing upload keystore. Keep it and its passwords outside Git. Verify `kz.yerek.aireply`, version `1.2.1` and code `5` in the generated bundle.
4. Compare the signing certificate with the fingerprint above, check that the production backend is `https://ai-reply.kz`, and confirm all four videos are included. The normal Gradle bundle is unsigned unless a signing configuration is supplied; use the signed output for Play.
5. Upload manually to the intended Play track and use the matching `whats-new-*.txt` files. Each is under Play's 500-character limit. Review Console validation and the final submission state.

Firebase client configuration is present. This Mac has no Web OAuth client ID; a release without one uses email sign-in and hides Google sign-in. Retain any correct `aireply.googleWebClientId` setting on the signing Mac. If Google sign-in is enabled, verify its Web client and the Android OAuth client for the Play app-signing certificate; an iOS OAuth client ID is not interchangeable.

## Verification

All four assets are separate H.264/yuv420p portrait MP4s at 720 × 1836, 30 fps, 78–85 seconds, with no audio or subtitle streams. Each was fully decoded and its contact sheet inspected. Captions are burned below the complete genuine recording. The optional voice scene shows real recording controls; test input is typed rather than fabricating a speech transcript.

Capture evidence and final playback checks are recorded in `docs/audits/2026-11-13-android-tutorial-capture.md` at the repository root. Offline playback in all four system languages, expanded controls, play/pause, replay, separate saved positions, closing/reopening and returning to the same onboarding step were verified on the final debug APK. All nine written steps and the voice guidance were reached by scrolling. Watching a recording grants no permission and sends no chat message.

The final source passed 1,021 unit tests in 83 suites, with no failures or skips. `lintRelease` reported 0 errors and 199 warnings; `assembleDebug` and the unsigned `bundleRelease` passed. These checks do not replace the signing and Play Console validation on the upload Mac.

This handoff is not evidence of a signed upload or publication. Source changes do not alter backend billing, prices, device limits or payment-provider settings. Build outputs, keys and raw recordings stay outside Git.
