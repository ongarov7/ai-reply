# Live speech caption timing — 18 November 2026

A metadata-only experiment with the deployed provider client confirmed that
`gpt-live-transcribe` returns partial transcription before the audio-turn commit.
The existing backend forwards those deltas unconditionally; only canonical
completion is correlated with the committed item ID.

The synthetic input was locally generated English TTS, streamed as PCM16 mono
24 kHz in paced 20 ms chunks, with the same `ru/kk/en` hints, `delay:low` and null
server VAD as production. No customer recording, message or credential was
printed or copied. A temporary isolated process inherited existing server
configuration and did not access a database or change the running service.

Ready arrived at 858 ms, input started at 859 ms and the first transcription
delta arrived at 1810 ms. All 29 deltas arrived before the manual commit at
16058 ms. The final canonical transcript arrived at 16808 ms. This is a single
synthetic English check, not a multilingual quality or physical microphone test.

The evidence supports using the existing protocol for build 31: publish existing
partial text to App Group storage and show it in the keyboard while recording;
keep the microphone active until the user chooses Done (or the hard recording
limit), then commit once and replace partial text with the canonical result.
Intermediate commits, a new continuous protocol and a backend deployment are
not needed to achieve live captions.

Ignored reproducibility artifacts and timing metadata:
`outputs/release-1.3.0-31/voice-experiment/`.

Official behavior matches the
[OpenAI realtime transcription guide](https://developers.openai.com/api/docs/guides/realtime-transcription).
