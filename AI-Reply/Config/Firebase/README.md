# Firebase configuration (iOS)

Put the Firebase iOS config of the app `kz.ai-reply.reply.keyboard.keyboard` here:

    AI-Reply/Config/Firebase/GoogleService-Info.plist

The genuine client config for project `ai-reply-4bf8f` is version-controlled so it travels with the repository. The "Firebase config" build phase copies it into the main app and checks its BUNDLE_ID. Firebase is not included in the keyboard extension.

From the repository root, run `python3 tools/validate_firebase_config.py`. See `docs/FIREBASE_SETUP.md` for APNs and backend requirements. Service account keys and Apple `.p8` files must stay outside Git.
