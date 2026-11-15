# Android authentication hotfix 1.2.3 (7)

Prepared on 15 November 2026, on top of the production 1.2.2 configuration.

## Changes

- The app and keyboard always use the light palette, independently of Android
  night mode, the host app's theme or a previously saved Dark preference. The
  appearance selector was removed. Launch and system-bar styles are light;
  Android's automatic Force Dark is disabled on supported API levels.
- Theme content inherits `onBackground`; cards supply `onSurface`.
  Authentication screens explicitly paint their background, keeping consent
  disclosure, checkbox labels and authentication headings readable.
- Tapping a consent label toggles its checkbox. Both consents remain required.
- Email requests and code verification show progress and disable their input
  while a request is running.
- Debug and release use the same Google availability check. Release APK/AAB
  packaging refuses a missing Web OAuth Client ID or the known iOS client ID;
  static analysis and unit tests still work without external OAuth setup.
  The actual public production Web Client ID is included as the default, so
  the signing Mac cannot omit Google merely by lacking a local property.

## Email delivery incident

The production API accepted the two requests at 09:23:26 and 09:23:59 UTC
(14:23 in Almaty). Resend showed `Delivery Delayed`; its diagnostic contained
Gmail `452 4.2.2` / `Mailbox Full`. The recipient had exhausted storage. The
same server and email endpoint serve iOS and Android. The recipient must free
space or use another address, then request a fresh code after the old codes
expire. No recipient address, OTP, API key or email body is recorded here.

## Google configuration

On 15 November 2026, the **Web application** OAuth client
`AI Reply Android` was created in Google project `ai-reply-4bf8f`
(`307300959376`). Its public Client ID is the checked-in Android build default:

```text
307300959376-50l6atnfem2hsi62uguhm9hisrncnlh8.apps.googleusercontent.com
```

Existing Gradle properties, `local.properties` and `GOOGLE_WEB_CLIENT_ID`
override the default in that order; an explicit empty override is rejected
when packaging a release. Do not reuse the separate iOS OAuth client.

The production service initially had only `GOOGLE_CLIENT_ID_IOS`. The operator
has now added the matching Web ID to its existing environment:

```text
GOOGLE_CLIENT_ID_WEB=307300959376-50l6atnfem2hsi62uguhm9hisrncnlh8.apps.googleusercontent.com
```

The backend image, data mounts and other environment values were preserved.
Production and sandbox health checks passed after the change, and production
advertises Google sign-in. The server backup is
`/home/ai-reply-backups/google-web-20261115T105128230219Z`.

Three Android OAuth clients were also created for `kz.yerek.aireply`, using
the three SHA-1 certificate fingerprints listed in Play Console (prefixes
`F4:B9`, `7A:61` and `D7:C5`). Verify sign-in with the application downloaded
from Play; an upload-key or local debug-key check alone does not establish
that the distributed application's certificate is accepted.

The audience is External and the publishing status remains Testing. Google's
[Manage App Audience documentation](https://support.google.com/cloud/answer/15549945?hl=en)
and [OAuth app state overview](https://developers.google.com/identity/protocols/oauth2/production-readiness/overview)
explicitly exempt Sign in with Google/basic identity (`openid`, `email`,
`profile`) from the Testing test-user allowlist. The current Credential
Manager flow requests only an ID token and no additional OAuth permissions;
therefore publishing the OAuth app is not required specifically to remove
that allowlist restriction. This exception would not apply to additional
OAuth scopes. No OAuth app publishing action was taken.

Google sign-in on a physical Android device with Google Play services remains
unverified. Cloud configuration, a healthy backend and a visible button do not
replace that check.

## Build and sign

On the Mac with the existing upload key, from its existing repository:

```sh
git switch main
git pull --ff-only origin main
cd AI-Reply-Android
./scripts/build-play-production.sh /absolute/path/existing-upload.jks EXISTING_ALIAS
```

The helper uses Android Studio's bundled JDK by default and asks for signing
passwords interactively. SDK 36 and the pinned Gradle dependencies must already
be installed: the helper builds offline. If dependencies are missing, sync the
project online in Android Studio first. Check any existing Google Client ID
overrides against the production ID above before building.

The helper forces production, version 1.2.3 and code 7, preserves older AABs,
and verifies the existing upload certificate. It does not upload or publish.
The expected output is:
`app/build/play-production/AIReply-1.2.3-code7-production-signed.aab`.
Code 7 has now been submitted to Play; do not upload it again. Future releases
need a higher version code.
The original upload key is present on this Mac at the ignored path
`outputs/store-release-2026-10-10/signing/ai-reply-upload.p12`, alias
`ai-reply-upload`, with the existing password file beside it. The earlier
statement that the key was unavailable was incorrect: the `.p12` file had been
overlooked. Its certificate was verified against the existing Play upload
certificate (`55:D8:6D:5B:8C:5F:DE:F2:53:F5:B7:B2:A1:16:49:95:3D:25:8C:CD:93:4E:F1:DA:B9:97:88:72:70:F9:87:DC`).
An unnecessary replacement request was submitted and then cancelled in Play
Console; the pending replacement banner disappeared. Use the original key.
A protected local copy is kept outside the repository under
`~/Library/Application Support/AI Reply/Android Signing/existing-play-upload/`.
Never commit the keystore or password file.

## Play Console inspection

Production 1.2.3 (7), release 3, was submitted through the owner's Play Console
on 15 November 2026. Console shows "Изменения на проверке" for one production
change, with 100% rollout after approval and managed publishing off. At
submission the automatic common-problem checks were running (up to 14 minutes);
Google will send the change onward automatically if those checks pass.
Production 1.2.2 (6) remains the published release while review is pending.
Countries remain Kazakhstan, Kyrgyzstan and Uzbekistan. Device support is
unchanged. There were no blocking errors; the one warning recommends native
debug symbols. ReTrace mapping is attached.

Pro Monthly and Pro Yearly each have one active base plan; legacy Base Monthly
has none. No subscription or pricing configuration was changed. The attempted
service-account API submission uploaded and staged code 7 but validation
returned 403; it was not committed. The actual submission used the owner's
Console account instead. Existing closed testing tracks were not changed.

The dashboard notification says to confirm trader status by 1 February 2027.
Under Account settings > Developer account > About you, the current status is
non-trader. The declaration dialog offers trader or non-trader; no declaration
was submitted. This is a separate account declaration, while the two Pro base
plans are already active.

The owner must review the correct status for the business and its distribution
regions. Google's [trader declaration guidance](https://support.google.com/googleplay/android-developer/answer/17594752?hl=en)
describes the criteria, account and app-level declarations, public contact
details, and the trader's legal commitment. Do not infer the declaration solely
from whether a subscription exists.

## Verification

- Debug and release JVM suites: 1,033 tests each, 84 suites, zero failures,
  errors or skipped tests.
- Final debug/release Kotlin compilation and `lintRelease`: successful;
  lint has zero errors and 199 existing warnings.
- Final `assembleDebug`: successful, production API, version 1.2.3/code 7.
- Release gate with the actual production default: default accepted; explicit
  empty, known iOS and malformed IDs rejected.
- Emulator QA: the app and keyboard stay light under system night mode and
  a persisted legacy Dark preference. Consent text, both checkbox labels,
  sign-in title and email input remain visible; a valid address enables
  Continue. QA did not request an email or create an account.
- Independent code review found no P1/P2 regression in the hotfix.

The fresh production AAB passed the guarded build, R8, release Google setup
gate, bundletool validation and signature verification. Its bundled manifest
is `kz.yerek.aireply`, version 1.2.3/code 7, not debuggable, SDK 26/36; the DEX
includes the production Web Client ID. Fresh targeted Android authentication
tests passed (22 tests), as did the backend Google authentication and billing
tests. The Pro Monthly and Pro Yearly plans were confirmed active through Play.

The AAB signed with the original upload key is
`AI-Reply-Android/app/build/play-production/AIReply-1.2.3-code7-production-signed.aab`,
14,442,549 bytes, with SHA-256:

```text
61825b9f9593270d8203b47f37d77c1d6afaf146d182a8bcecf383a91ae33960
```

Current artifact metadata and release evidence are saved under the ignored
directory `outputs/android-release-2026-11-15/`. A separate AAB signed with
the cancelled replacement key must not be uploaded. End-to-end Google sign-in
and an actual purchase remain unverified: no Android device was available,
and the owner requested release without that physical-device check.
