# Apple Watch: App Review

What changes for review, and what still has to be done by hand in App Store Connect.

## Done in the code

| Requirement | Where |
|---|---|
| 5.1.2(i): disclose third-party AI and get explicit permission | First-use screen on the watch names OpenAI, with Allow and Not now, before anything is recorded. It can be withdrawn on the watch. |
| 5.1.1: purpose string | `NSMicrophoneUsageDescription` in the watch app, in en, ru, kk and uz, names OpenAI |
| In-app disclosure | `legal.consent.ai` and `settings.privacy.body` mention the Apple Watch microphone |
| Privacy policy | `/privacy` in all four languages has an Apple Watch paragraph (`legal_texts.go`). **Needs a backend deploy.** |
| Privacy manifest | New `AIReplyWatch/PrivacyInfo.xcprivacy`; `NSPrivacyCollectedDataTypeAudioData` added to the iPhone app's manifest |
| 4.4.1: keyboards | The keyboard works without a watch and without Full Access as before. It never presses Send |

The privacy policy version was not raised, on purpose. Raising `legal.PrivacyVersion` makes
every user accept the terms again, and this feature has its own permission screen at the point
of use. If legal review prefers a new version, it is one constant.

## By hand in App Store Connect

1. **App Privacy**: add **Audio Data**, purpose **App Functionality**, linked to the user, not
   used for tracking. Recordings pass through OpenAI, whose API may retain data for a limited
   time, so "collected" is the honest answer under Apple's definition.
2. **Screenshots**: Apple Watch screenshots are required once the build contains a watch app.
3. **Description**: mention the Apple Watch companion.
4. **Deploy the backend first**, so the published policy matches the build.

## Notes for the reviewer

Paste into App Review Information ▸ Notes. Attach a screen recording: a reviewer may not have a
paired watch.

```text
Apple Watch companion (optional feature)

AI Reply includes a watchOS companion that lets the user dictate an instruction for the AI Reply keyboard. The app works fully without an Apple Watch.

How to test
1. Sign in with the demo account above and enable the AI Reply keyboard with Full Access.
2. In any messaging app copy a message, focus the text field and switch to the AI Reply keyboard.
3. Open AI Reply on the paired Apple Watch, tap Speak, say what the reply should contain and tap Stop.
4. Review the recognised text on the watch and tap Send.
5. The generated reply appears in the text field. The app never sends a message; the user reviews it and sends it.

Microphone and third-party AI
The microphone is used only between Speak and Stop, after a user tap. Before the first recording the watch shows a screen stating that the recording is sent through the iPhone and the AI Reply server to OpenAI (USA) to be turned into text, and asks for permission. The user can decline and type instead, and can withdraw permission on the watch. AI Reply does not store the audio or the transcript.

The Apple Watch app has no account of its own and uses the iPhone app's session.

A demo video is attached in case a paired Apple Watch is not available.
```

## Before submitting

- [ ] Real device test: microphone, Russian, English, Kazakh, through the production server
- [ ] Backend deployed with the new policy text
- [ ] App Privacy updated
- [ ] Watch screenshots and demo video attached
- [ ] Watch app version and build match the iPhone app
