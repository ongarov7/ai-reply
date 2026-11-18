# iPhone Voice: realtime transcription

The app records; the keyboard never accesses the microphone. Streaming only turns
an instruction into text. The existing `/api/v1/ai/reply` or `/ai/compose` builds
the answer using existing templates, account context, subscriptions and reply
limits. No OpenAI key or provider session credential is returned to the app.

## Configuration

- `TRANSCRIBE_ENABLED=true` enables the existing voice feature.
- `REALTIME_TRANSCRIBE_ENABLED=true` enables streaming (default true).
- `OPENAI_REALTIME_TRANSCRIBE_MODEL=gpt-live-transcribe` is the server-selected model.
- The existing `OPENAI_API_KEY` and `OPENAI_BASE_URL` are reused.
- `RATE_TRANSCRIBE_PER_MINUTE` and `LIMIT_TRANSCRIBE_PER_DAY` apply to both paths.
- `GET /api/v1/config` advertises `features.voice_realtime_transcription` only
  when streaming is enabled and the provider implements it. Missing/false means
  that the app must use the existing bounded-file upload.

Deploy the backend before enabling streaming in a released iPhone app. Its
reverse proxy must forward WebSocket upgrades and allow at least 70 seconds for
the route. Caddy `reverse_proxy` supports upgrades by default; confirm any custom
path matchers include `/api/v1/ai/transcribe/realtime`. Do not put bearer tokens in
URLs. Record the deployed image, source checksum and backup location in the
release notes after activation.

## Client contract

Open `wss://<backend>/api/v1/ai/transcribe/realtime` with the same
`Authorization: Bearer <access_token>` and installation/device headers as other
account API requests. Authentication and current legal consent are checked
before the upgrade; expired sessions receive the normal HTTP authentication
error. Browser requests must have a same-origin Origin; native clients omit it.

Send JSON text frames:

```json
{"type":"start","request_id":"<UUID>","languages":["ru","kk","en"]}
```

Wait for `{"type":"ready"}` before streaming:

```json
{"type":"audio","audio":"<base64 signed PCM16 little-endian mono 24000Hz>"}
{"type":"commit"}
```

At most 64 KiB of decoded audio per frame, 40 seconds total. The start frame must
arrive within 5 seconds. One committed audio turn per connection; do not send
further audio or another commit. `{"type":"cancel"}` abandons the turn. The
connection has a hard 70 second lifetime, including setup and final transcript.
The provider interaction is bounded to 65 seconds, including a 10 second setup.
The iPhone keeps recording through speech pauses. It sends its one commit when
the user chooses Done or the 40-second recording limit is reached. An initial
20 seconds without speech may stop the recording without generation.
Invalid PCM, empty/short turns, oversized turns and arbitrary provider events
are rejected. The service allows at most one live turn per account at a time.

Server events:

```json
{"type":"ready"}
{"type":"delta","text":"incremental text"}
{"type":"completed","text":"canonical final transcript"}
{"type":"error","code":"VOICE_LIMIT_REACHED"}
```

Append delta text for display; replace it with the final transcript. Generate an
answer only after `completed`, never from partial speech. Error codes come from
the existing HTTP error vocabulary. A response with an empty final transcript
is valid silence; do not turn it into a generation request. Completion/error
ends the socket. Successful `request_id` values share the existing 10 minute
in-memory replay cache: a replay may immediately return `completed` without
`ready`, more audio or an extra charge.

## Provider behavior and privacy

The backend connects to `/v1/realtime?intent=transcription`, then sends
`session.update` with transcription type, 24kHz PCM input, the configured model,
`languages` and `turn_detection: null`. The iPhone sends one commit on Done or its
hard recording limit; short speech pauses do not commit or finish a turn.
Live transcription deltas arrive before that commit and can update the keyboard
while capture continues. Provider events are filtered into the small contract above;
provider errors, credentials and arbitrary event payloads are never forwarded.
Only the completion matching the committed item ID is accepted.

`gpt-live-transcribe` does not return detected-language metadata. Input hints are
filtered through the existing ru/kk/en/uz allowlist. Output language continues to
be decided by the existing generation workflow and explicit user instructions.
The app can fall back to `/api/v1/ai/transcribe` with an M4A/WAV recording and
`gpt-transcribe` when streaming is unavailable.

Audio and transcript stay in memory, are never written to a file or usage log,
and are bounded by frame/turn/session limits. The existing short-lived replay
cache retains the final text in process memory for retry only. Usage events
record mode, account, model, seconds and outcome; streaming does not spend reply
quota. There is no DB migration.

Official API checked for this implementation on 18 November 2026:

- [Realtime transcription](https://developers.openai.com/api/docs/guides/realtime-transcription)
- [GPT-Live-Transcribe](https://developers.openai.com/api/docs/models/gpt-live-transcribe)

## Executable verification

Provider tests use an actual local WebSocket peer and verify auth, session type,
format, multilingual hints, null VAD, deltas, committed item matching and final
text. They reject malformed base64, odd PCM bytes, empty commit, arbitrary
commands, oversized frames and turns over 40 seconds. Integration tests run the
real HTTP middleware and verify auth/consent/feature gating, replay idempotency,
daily cap, zero reply quota charge and no private transcript in logs. No live
OpenAI request or microphone-quality test is implied by these automated tests.

## Dictated instruction length

A 40-second turn can exceed the typed instruction limit (400 by default). The
iOS app sends `instruction_source: "voice"` on `/api/v1/ai/compose` and
`/api/v1/ai/reply` when `/api/v1/config` publishes `features.voice_instruction`;
such an instruction may use `max_voice_instruction_length` characters
(`limits.VoiceInstructionChars` = 1 000, never below the typed limit). Typed
instructions keep `max_instruction_length`; any other `instruction_source`
value is treated as typed. Request bodies still reject unknown fields, so the
app never sends the field to an older server and instead cuts the dictation
after the last whole word that fits.
