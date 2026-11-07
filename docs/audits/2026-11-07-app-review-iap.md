# iOS 1.2.0 (23) — purchase review

The November 5 rejection for submission `5617048b-f0e0-4d23-b5d1-798833e9aeb8`
is Guideline 2.1(b), Information Needed. Apple could not locate the five products
and requested navigation steps, sandbox readiness and storefront restrictions.
The message does not identify a failed charge.

On November 7, App Store Connect visibly showed the Paid Apps Agreement, bank
information and both US tax forms as Active. The previous release record and
review notes explicitly recorded Processing at submission time. Apple requires
the paid agreement to be active for review purchases as well:
https://developer.apple.com/help/app-review/before-submitting-for-review/configure-in-app-purchases/.

The 30-reply consumable was Developer Rejected and available in Kazakhstan,
Kyrgyzstan and Uzbekistan. Its review notes now disclose these storefronts and
explain the exact navigation and Buy once action. Availability was not changed.

## Source and validation

The local `main` contained build 19. The prior build 22 subscription-management
fixes were preserved in `stash@{0}` from the November 7 pull. Only those fixes,
their five localized messages and eight management tests were integrated into
the current source. The current main catalogue retry, late-config loading and
account-change protections were retained. The stash was not popped or deleted.

App and keyboard use version 1.2.0, build 23, and `https://ai-reply.kz`.
No payment product IDs, prices, signing team or bundle IDs were changed.

50 purchase, subscription-display and management tests passed with no failures
or skips on iPhone 17 Pro / iOS 26.5 Simulator. The xcresult summary was read
independently. The Release archive succeeded and strict signature verification
passed. Both archived bundles' versions and production API were verified.
No live Apple sandbox charge or physical iPad checkout is claimed by these tests.

Artifacts and upload evidence: `outputs/release-1.2.0-23/` (ignored by Git).
The upload allows both TestFlight and App Review; internal-testing-only is not set.

## Review navigation

1. Sign in with the review email and fixed code already saved in App Review
   Information. Choose Continue with Email, request a code and enter the saved
   review code. This account does not require access to an inbox.
2. Complete consent and complete or skip onboarding. Keyboard activation and
   Full Access are not required to open purchases.
3. Tap the balance / available-replies card on Home, or open Settings using the
   gear icon, then Balance and access.
4. Select Monthly for Base Monthly and Pro Monthly; select Yearly for Base
   Yearly and Pro Yearly. Tap Subscribe on the desired product.
5. Scroll below subscriptions to Without a subscription, then use Buy once
   for the 30-reply pack. The server localizes the product name by app language.
6. Manage subscription is for existing Apple subscriptions, including expired
   history; it is separate from purchasing a subscription. Restore purchases
   is available on the same screen.

Apple submission guidance requires first purchases of each type to accompany
an app version and the subscription group:
https://developer.apple.com/help/app-store-connect/manage-submissions-to-app-review/submit-an-in-app-purchase/.

Upload processing, TestFlight group assignment and the final review-submission
status are recorded after they are visibly confirmed in App Store Connect.
