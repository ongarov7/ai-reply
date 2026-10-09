# Backend deployment environment — 9 October 2026

This checklist compares the current `notification` branch with the server environment pasted by the owner on 9 October. Codex has not inspected private live server files or performed a live deployment. The owner has now deployed the updated backend through Docker. The local backend `.env` is different from the pasted server environment: it has no Resend, Google, Apple or Firebase settings. Do not copy the local `.env` to production.

Earlier read-only live check on 9 October: public `/healthz` and `/api/v1/config` returned HTTP 200 with staging/demo settings and without the new notification flags. After the owner's Docker deployment, Codex repeated only those public GET requests: both returned HTTP 200; `/api/v1/config` now reports `demo_mode=false`, `payment_mode=off`, `purchases=false`, and enabled `installations`, `push_notifications`, `account_deletion` and `ai_reports`. The owner supplied successful production configuration validation and logs reporting migrations 0011–0014 applied. An initial owner request returned 502 immediately after container startup, but the later public checks passed; the exact cause of that temporary 502 was not established. No live server shell, private file or production data was accessed by Codex. Physical-device authentication, deletion and notification delivery still need testing.

## Required changes to the pasted server environment

Keep existing database paths and application settings unless a change below is required. Apply these values to the existing server `.env`, not by replacing the whole file with `.env.example`:

Do not append duplicate assignments for existing variables. The Go `-env` loader preserves existing process variables and, when reading a file directly, the first assignment wins. Replace each existing line once; do not rely on Docker and direct Go startup interpreting duplicates the same way. The owner's latest paste already has the correct `APPLE_CLIENT_ID`; keep it unchanged.

```dotenv
APP_ENV=production
PUBLIC_BASE_URL=https://ai-reply.kz
AUTH_DEMO_MODE=false
LEGACY_API_ENABLED=false
PAYMENT_MODE=off
PAYMENT_DEMO_CHECKOUT=false
TRUST_PROXY=true
ADMIN_SECURE_COOKIES=true
SIMULATOR_ENABLED=false

APPLE_CLIENT_ID=kz.ai-reply.reply.keyboard.keyboard

GOOGLE_CLIENT_ID_IOS=307300959376-cmqofin5ebbdjupepp5ff5u1ehhi84eo.apps.googleusercontent.com
GOOGLE_CLIENT_ID_WEB=<WEB_OAUTH_CLIENT_ID.apps.googleusercontent.com>

FIREBASE_SERVICE_ACCOUNT_FILE=/run/secrets/firebase-service-account.json
PUSH_NOTIFICATIONS_ENABLED=true
PUSH_WORKER_ENABLED=true

LEGAL_OPERATOR_NAME=<ACTUAL_OPERATOR_NAME>
LEGAL_OPERATOR_DETAILS=<ACTUAL_OPERATOR_REGISTRATION_AND_CONTACT_DETAILS>
CONTACT_EMAIL=<MONITORED_SUPPORT_MAILBOX>
```

`TRUST_PROXY=true` assumes the backend is reachable only through the configured trusted reverse proxy; keep port 8080 off the public interface. Use plain URLs in `.env`, without Markdown brackets. The pasted administrator password is below the production minimum of 16 characters; set a new strong password on the server. The credentials pasted into chat must be rotated by the owner, including OpenAI, Resend, administrator, JWT and legacy signing secrets. Changing JWT secrets invalidates existing sessions; coordinate that change with the deployment.

The pasted `GOOGLE_CLIENT_ID_IOS=kz.yerek.replykeyboard` is a bundle identifier, not an OAuth client ID. Both Google IDs must come from the current Google Cloud project. No Google OAuth client secret is used by the current backend ID-token verifier. Keep iOS build settings aligned with `GOOGLE_CLIENT_ID_IOS`; Android's `aireply.googleWebClientId` must match `GOOGLE_CLIENT_ID_WEB`.

The earlier pasted `APPLE_CLIENT_ID=kz.ai-reply.reply.keyboard` did not match the signed app. The latest paste correctly uses **`kz.ai-reply.reply.keyboard.keyboard`**. The earlier mismatch explains a possible `401 INVALID_ID_TOKEN` / `audience_mismatch`; confirm the running server has reloaded the corrected audience and repeat sign-in on TestFlight. It is not proof that the live sign-in issue has already been fixed.

### Append only variables absent from the latest paste

```dotenv
FIREBASE_SERVICE_ACCOUNT_FILE=/run/secrets/firebase-service-account.json
PUSH_NOTIFICATIONS_ENABLED=true
PUSH_WORKER_ENABLED=true
PAYMENT_DEMO_CHECKOUT=false
SIMULATOR_ENABLED=false
LEGAL_OPERATOR_NAME=
LEGAL_OPERATOR_DETAILS=
```

The Firebase path is inside the container and only works after mounting the real server JSON read-only as described below. Fill the legal operator's actual name and registration/address details before store release. Replace the existing Google iOS ID and production/demo flags using the full block above; the Google Web ID remains empty until the Android Web OAuth client is created.

## Credentials to add

| Purpose | Missing from the pasted server environment | Where to configure |
|---|---|---|
| FCM HTTP v1 sender | `FIREBASE_SERVICE_ACCOUNT_FILE` | Mount the existing private JSON read-only on the server |
| Google iOS sign-in | Correct `GOOGLE_CLIENT_ID_IOS` | Backend `.env` and iOS OAuth build settings |
| Google Android sign-in | `GOOGLE_CLIENT_ID_WEB` | Backend `.env` and Android `aireply.googleWebClientId` |
| Apple token revocation on account deletion | `APPLE_TEAM_ID`, `APPLE_KEY_ID`, `APPLE_PRIVATE_KEY` | Backend `.env`, using a Sign in with Apple key |
| iOS push transport | APNs authentication key | Firebase Cloud Messaging settings; not the backend `.env` |
| Legal operator | `LEGAL_OPERATOR_NAME`, `LEGAL_OPERATOR_DETAILS` | Actual owner details in backend `.env` |

For Apple revocation, supply all three together:

```dotenv
APPLE_TEAM_ID=2PK6339Q47
APPLE_KEY_ID=<SIGN_IN_WITH_APPLE_KEY_ID>
APPLE_PRIVATE_KEY=<PEM_WITH_ESCAPED_NEWLINES_OR_BASE64>
```

`APPLE_PRIVATE_KEY` is the key's contents in the supported format, not a filesystem path. An APNs-only key cannot replace a Sign in with Apple key. Without revocation credentials, local account deletion still runs but Apple authorization is not revoked; startup warns and the legal text reflects that limitation.

Alternatively, FCM accepts the three variables `FIREBASE_PROJECT_ID`, `FIREBASE_CLIENT_EMAIL`, `FIREBASE_PRIVATE_KEY`. Choose either these variables or the mounted JSON. The file option is recommended for the existing Docker deployment; do not set both sources. Client `GoogleService-Info.plist` / `google-services.json` cannot authenticate the server sender.

Resend is already present in the pasted server environment. Confirm the new rotated key and verified sender domain; it is absent only from the laptop's `.env`. Other new template variables for retention, batch sizes, rate limits and AI features have defaults and do not need new credentials. `REVIEW_LOGIN_EMAIL` and `REVIEW_LOGIN_CODE` are optional review access; use only when preparing store review and clear them afterwards.

## Owner's executable deployment steps

1. Back up the existing SQLite database consistently, including committed WAL data. Preserve the current data volume and user records. The new backend includes migrations 0011–0014; do not initialize an empty replacement database.
2. On the Linux Docker host, create `/srv/ai-reply/secrets` outside the checkout, then transfer the private FCM service-account file there through a secure channel. Its original filename can stay `ai-reply-4bf8f-31db4f3ee80d.json`. These are commands for the owner, not commands Codex has run on the server:

   ```sh
   mkdir -p /srv/ai-reply/secrets
   chmod 700 /srv/ai-reply/secrets
   # After uploading the JSON:
   chown 10001:10001 /srv/ai-reply/secrets/ai-reply-4bf8f-31db4f3ee80d.json
   chmod 600 /srv/ai-reply/secrets/ai-reply-4bf8f-31db4f3ee80d.json
   ```

   The existing Dockerfile runs the backend as UID 10001, which must be able to read the file. Keep the host directory root-owned and inaccessible to other host users. This assumes the existing rootful Docker setup without user-namespace remapping; mapped/rootless setups need matching host ownership. Do not send the key through Git or bake it into an image.
3. The read-only bind block is now enabled in `ai-reply-back-end/docker-compose.yml` at the owner's explicit request after confirming the server upload. Before starting, verify the host file exists and is readable by UID 10001. The existing database volume stays mounted alongside it:

   ```yaml
   - type: bind
     source: /srv/ai-reply/secrets/ai-reply-4bf8f-31db4f3ee80d.json
     target: /run/secrets/firebase-service-account.json
     read_only: true
     bind:
       create_host_path: false
   ```

4. Apply the environment changes above and replace placeholders with actual values. Preserve the existing Compose project name and data volume. From the existing deployment directory (for the owner's server, `/home/ai-reply/ai-reply-back-end`), validate and build before starting the service:

   ```sh
   cd /home/ai-reply/ai-reply-back-end
   docker compose config --quiet
   docker compose build backend
   docker compose run --rm --no-deps backend -check
   ```

   `config --quiet` validates without printing the rendered environment or secrets. The one-off container uses the new image, real environment and read-only JSON mount; `-check` opens no database, port or provider connection. Resolve all errors and inspect warnings. These commands do not restart the running service. A previous binary may not support `-check`.

5. Deploy/restart through your existing server procedure. Codex has not deployed or restarted the live backend. Confirm `/healthz`, `/api/v1/config`, `/privacy`, `/support` and `/account/delete`, then test OTP delivery, Google/Apple sign-in, AI consent refusal, Free limits, hidden subscription plans and deletion with a test account.
6. Test push on physical devices after Firebase APNs setup and backend activation: permissions allowed/denied, token renewal, foreground/background/tap, logout and deletion. Use a single test installation; do not broadcast to production users for this check.

Never paste actual `.env` values, private JSON or `.p8` contents into reports or logs. Client configuration files remain shareable; private sender and Apple keys remain outside Git.
