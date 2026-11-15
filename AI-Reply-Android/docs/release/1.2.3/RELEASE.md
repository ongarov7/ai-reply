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
Confirm that code 7 is still unused in Play Console before uploading.
The upload keystore was not found in this Mac's Downloads, Documents, Desktop
or workspace. The existing key remains on the owner's other Mac; no signing
key reset or replacement was performed.

## Play Console inspection

The production release is still 1.2.2 (6), rolled out to 100%. Pro Monthly and
Pro Yearly each have one active base plan; legacy Base Monthly has none. No
subscription or pricing configuration was changed during this inspection.

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

Local debug APK and screenshots are under the ignored directory
`outputs/android-auth-hotfix-2026-11-15/`. The APK is for UI review, not Play
upload. The final production AAB passed the guarded build, R8 and release
checks. The actual bundled manifest is `kz.yerek.aireply`, version 1.2.3/code 7,
not debuggable; its DEX includes the real production Web Client ID. The AAB is
unsigned, 14,425,271 bytes, with SHA-256:

```text
cac6b865b88ef78586654fc47bcfc8f7294a70e9edaa7a5358593fc1aa9c5751
```

Artifact metadata and the decoded bundled manifest are saved in the local
output directory. The owner will sign and upload from the existing signing
Mac. No upload-key reset is needed. No release has been signed, uploaded or
published here. End-to-end Google sign-in remains unverified.
