# Android 1.2.5 (9): subscriptions

Requested Google Play prices for Kazakhstan: unlimited Pro Monthly **990 KZT**
with monthly automatic renewal, and Pro Yearly **11,880 KZT** with yearly
automatic renewal (990 × 12). New accounts receive **10 replies once**, shared
across platforms. New Android sales of the 500-reply pack are retired; purchased
balances and historical receipt verification remain intact. The 30-reply pack
and other platforms' paid prices remain available under their existing terms.

The app displays Google Play's localized price and verifies it against the
backend catalogue before launching a purchase. Only automatically renewing P1M
and P1Y base plans are eligible; prepaid and four-week plans cannot be presented
as monthly automatic renewal. Backend expected prices use
`IAP_GOOGLE_PRO_MONTHLY_KZT_PRICE_MINOR=99000` and
`IAP_GOOGLE_PRO_YEARLY_KZT_PRICE_MINOR=1188000`. Migration 0044 changes the previous
welcome default from 20 to 10 without reducing balances already granted.

## Validation and artifact

Both debug and release JVM suites passed **1,036 tests**, with no failures,
errors or skips. Release lint passed with zero errors and 178 warnings.
The build used Homebrew OpenJDK 23 with an explicit `org.gradle.java.home`
override; the older local JDK has outdated Kazakhstan timezone data.
The backend full `go test ./...` suite passed, including migration preservation
and platform-specific monthly/yearly pricing tests.

The signed production bundle uses package `kz.yerek.aireply`, version `1.2.5`,
code `9`, and `https://ai-reply.kz`. Bundletool validation and independent
manifest checks passed. The original Google Play upload certificate matches:
`55:D8:6D:5B:8C:5F:DE:F2:53:F5:B7:B2:A1:16:49:95:3D:25:8C:CD:93:4E:F1:DA:B9:97:88:72:70:F9:87:DC`.

Local artifact: `outputs/android-pricing-2026-11-25/AIReply-1.2.5-code9-production-signed.aab`.
SHA-256: `2da20ca640108aeb47c1e37fa278b7838d2abc8a939b5b61dd00f24233976e02`.
Evidence and release notes are in the same ignored output directory. Secrets,
signing keys and binaries are excluded from Git.

A physical-device purchase and renewal have not been exercised during this
release. Successful automated checks do not establish that device test.

## Publication

The service account accepted the bundle into a temporary Publisher API edit,
but validation returned HTTP 403. The edit was deleted without committing;
uploading or staging in that edit did not publish a release. Final Play Console
submission and saved regional prices must be verified separately.

Backend deployment is complete and independently verified. The owner Console
could not be operated to completion: native scrolling returned
`noWindowsAvailable`, the page renderer stopped updating, and restarting Chrome
also encountered a ScreenCaptureKit error. No store price or purchase-option
change and no production submission were confirmed.

To complete in the owner Console:

1. Open [monthly pricing](https://play.google.com/console/u/0/developers/4669560719422191406/app/4974342352292703805/subscriptions/s/kz.aireply.pro.monthly/base-plans/b/monthly),
   filter Kazakhstan, change 3,990 to **990 KZT**, and save.
2. Open [yearly pricing](https://play.google.com/console/u/0/developers/4669560719422191406/app/4974342352292703805/subscriptions/s/kz.aireply.pro.yearly/base-plans/b/yearly),
   change Kazakhstan to **11,880 KZT**, and save. Keep automatic renewal.
3. In [one-time products](https://play.google.com/console/u/0/developers/4669560719422191406/app/4974342352292703805/one-time-products),
   deactivate the `kz.aireply.pack500` purchase option without deleting the
   product or changing `pack30`.
4. Create a production release, upload the signed AAB above, add the localized
   release notes from the output directory, review it, then send for review from
   [Publishing overview](https://play.google.com/console/u/0/developers/4669560719422191406/app/4974342352292703805/publishing).

Changing new-purchase base-plan prices does not by itself migrate existing
subscribers from their legacy price cohorts. No cohort migration was performed.
