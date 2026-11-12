# macOS 1.0.1 (2) publication — 13 November 2026

## Published installer

- URL: <https://ai-reply.kz/downloads/macos/AI-Reply-1.0.1.dmg>
- Size: 3,415,146 bytes.
- SHA-256: `70d6bfde8d09071feb461ce0b5d8bf5678148fc20e97d7443a60128368a7448b`.
- Universal arm64/x86_64; macOS 14 or later; bundle `kz.ai-reply.desktop`.
- Release API: `https://ai-reply.kz`.

The mounted image contains `AI Reply.app` and an `/Applications` shortcut. The
mounted binary matches the exported release. Developer ID signing, the stapled
app and image tickets, and Gatekeeper verification passed. Both Apple notary
submissions were accepted:

- App: `fad373d1-0cf9-4458-8fe3-033ea9ddc9c3`.
- DMG: `5d839616-ca78-43ad-a64c-9db8cfb64a8f`.

A complete download from the public URL returned HTTP 200, the disk-image MIME
type and matching size/hash. All four `/mac?lang=kk|ru|en|uz` pages link to this
image. The previous `AI-Reply-1.0.0.dmg` URL redirects to the new image.

The Swift suite passed 162 tests across 26 suites. Release build, archive and
export passed. Detailed local proofs and generated artifacts are ignored under
`outputs/macos-release-1.0.1-2/`; signing material stays outside the repository.

## Server update

The existing backend service was rebuilt and restarted with backups. Caddy now
serves only the installer directory at `/downloads/macos/`; other requests retain
the backend route. The only release environment change was `MACOS_DOWNLOAD_URL`.
Both primary and isolated sandbox health checks passed after restart. Existing
database volumes, users, subscriptions and credit grants were retained.

Server backups:

- Source: `/home/ai-reply-backups/macos-source-20261112T200403Z`.
- Runtime, environment and both databases:
  `/home/ai-reply-backups/20261112T200404Z`.
- Installer publication environment and Caddy configuration:
  `/home/ai-reply-backups/macos-installer-20261112T202009Z`.

Any rollback must preserve transactions written after deployment. Restore the
affected source/configuration from its backup and validate it before rebuilding
or reloading the existing service. Do not replace a live database with an older
backup merely to revert the installer URL.

## Payment boundaries

The final website/Mac lineup is a **one-time 500-reply pack for 990 KZT**,
a one-time 30-reply pack for 130 KZT, and unlimited Pro for 3,990 KZT/month
or 39,990 KZT/year. Pack replies do not expire and do not create a recurring
mandate. Migration `0040_web_reply_pack_500.sql` enables the existing 500-reply
product for web checkout and removes Base from new web offers. Legacy store
products, Base plan quantities, paid order/mandate snapshots and existing
balances remain unchanged.

The release app and primary website use the same account backend. Web BCC
payments update that account only after an authenticated final bank notification.
Browser return alone does not grant access. Automated integration coverage
checks the one-time 500-reply website purchase reaching an existing Mac session,
generation leaving 499 replies in both website and Mac views, duplicate bank
notification handling, and debit idempotency. Separate historical tests retain
Base contract/cancellation coverage. A mixed-provider regression checks that
Apple refunds preserve BCC access/credits and BCC cancellation/refunds preserve
Apple access/credits.

BCC and App Store keep separate notification handlers and provider records.
Existing Apple purchase configuration and device-limit flags were not changed.
The primary site's real BCC checkout remains disabled because production bank
credentials and approval are absent. Publishing this installer does not enable
real charges.

The `/sandbox` environment retains its separate database, sessions and test-bank
callback. Its Mac download URL is configured separately and defaults to empty;
it cannot silently distribute the primary release installer. A distinct signed
`kz.ai-reply.desktop.sandbox` test build targets `/sandbox`; it was not published
as the release download. Bank-confirmed sandbox fulfillment remains a separate
end-to-end check; creation of an order or display of the bank form is not proof
of a successful payment.
