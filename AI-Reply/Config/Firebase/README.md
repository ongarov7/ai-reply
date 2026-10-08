# Firebase configuration (iOS)

Put the Firebase iOS config of the app `kz.ai-reply.reply.keyboard.keyboard` here:

    AI-Reply/Config/Firebase/GoogleService-Info.plist

The file is git-ignored. The "Firebase config" build phase copies it into the app when it is present and checks that its BUNDLE_ID matches the app; without it the app builds and push stays off. See `docs/FIREBASE_SETUP.md`.
