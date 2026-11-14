# Windows installer publication — 14 November 2026

The owner authorized uploading the Windows installer to the existing website. This publishes the
tested Windows preview; it does not establish complete macOS parity or a completed manual Windows release gate.

## Published file

- Direct download: <https://ai-reply.kz/downloads/windows/AIReply-Setup.exe>.
- Immutable version: <https://ai-reply.kz/downloads/windows/AIReply-Setup-1.0.0.exe>.
- Checksum: <https://ai-reply.kz/downloads/windows/AIReply-Setup.exe.sha256>.
- Manifest: <https://ai-reply.kz/downloads/windows/windows-1.0.0.json>.
- Version: `1.0.0`, preview, unsigned, native x64/ARM64 payloads in one installer.
- Size: `95,283,498` bytes.
- Installer SHA-256: `e0641c91cc13be4944da17814c8feeeae6f90369006160d241a1dd8ff5290094`.
- Source commit: `6149449d84909aad29282041ba867126edd75cd0`.
- [Successful Windows workflow](https://github.com/ongarov7/ai-reply/actions/runs/43590900481).
- Artifact: `14112312131`, `AIReply-installer`.
- Artifact ZIP SHA-256: `14619c1ec366928bc889213d1080f0abca465ed151a656ec8b2f79e3776b11e8`.

The artifact was downloaded directly by the server. Both the outer GitHub artifact digest and its
embedded installer checksum were validated before publication. Only the expected executable and checksum
entries were read. The universal installer's PE header was checked; the native payload selection and
installer lifecycle were previously tested on the Windows x64 runner. No 95 MB download/upload passed
through the owner's Mac connection.

## Server change and preservation

Files are served from `/srv/ai-reply/downloads/windows/`. The stable executable name is a relative
symlink to the immutable version. Checksum file contents match their respective executable filenames.
The manifest explicitly records `release_channel: preview`, `signed: false` and
`manual_windows_qa_complete: false`.

Only this sibling handler was added to the existing Caddyfile:

```caddyfile
handle_path /downloads/windows/* {
    root * /srv/ai-reply/downloads/windows
    header X-Content-Type-Options nosniff
    header Content-Disposition attachment
    header Content-Type application/octet-stream
    header Cache-Control no-cache
    file_server
}
```

The candidate configuration was validated before atomic replacement and `systemctl reload caddy`.
The previous macOS route, TLS configuration and backend reverse-proxy fallback were retained. No
backend restart, backend source deployment, environment change or database operation was performed.
The main website's Windows download button/page was not added; the direct installer URL is available.

Backup and proof directory:
`/home/ai-reply-backups/windows-installer-1.0.0-20261114T140413Z`.
It includes the original/candidate Caddyfiles, original artifact, validated installer, baseline and
publication result. No temporary download URL or GitHub credential is included in the release manifest.

Verification passed:

- Stable and immutable URLs: HTTP 200, exact content length, binary MIME, attachment and nosniff headers.
- Both URLs: `Range: bytes=0-1` returned HTTP 206 and the executable `MZ` prefix.
- Full download through the public HTTPS URL, performed on the server: exact size and SHA-256 above.
- A nonexistent Windows file returned HTTP 404; directory browsing remains disabled.
- An independent request from the Mac confirmed public HTTP 200 and a two-byte HTTP 206 download.
- Website, production health and isolated sandbox health remained available.
- Backend container ID, image ID, start timestamp and restart count were unchanged.
- macOS 1.0.2 remained HTTP 200, `3,461,472` bytes, with unchanged SHA-256
  `14ae8bfe725cf600d3f6441af8bc1a7e661789be869a9305fdc38a02e659553f`.

Original Caddyfile SHA-256: `a83e080f2b11258057910e7f55301ef44c661abc036b80cecbc23884c97ef933`.
Published Caddyfile SHA-256: `481b5d76c99c20bbba1faf10bdcd3eb8157f221c2ad1c6260781f5d33d47d281`.

If this route needs rollback, restore only the saved Caddyfile after checking for newer administrator
changes, validate it, and reload Caddy. Do not restore older databases or backend images to undo a file link.

## macOS parity review

The Windows client implements the principal account/generation/clipboard/voice/context paths. It is
**not an exact design or functional copy of macOS 1.0.2**. A read-only comparison of current sources
confirmed these gaps; passing automated tests does not remove them:

1. **Website payment return:** macOS refreshes immediately on activation and has a bounded 120-second
   catch-up window for delayed bank notifications (`AirReplyCore/AccountStore.swift:117`,
   `AirReplyUI/AppCoordinator.swift:77`). Windows `BackendAdapter.cs:58` refreshes with a 10-second
   cache; Settings `SettingsWindow.cs:158` uses that same path. It has no equivalent activation/payment
   retry window. Users may need to reopen/refresh; a recently cached balance can remain visible.
2. **Context editing and temporary documents:** macOS supports inline profile editing and transient
   attachments (`AdvancedContextView.swift:121`, `QuickBarModel.swift:493`). Windows's bar only selects
   a saved profile (`QuickBarView.axaml:57`); importing from Settings first requires a saved profile
   (`SettingsWindow.cs:206`).
3. **Voice presentation:** macOS has a waveform and elapsed/max timer (`QuickBarView.swift:299`).
   Windows currently has one level indicator without the timer (`QuickBarView.axaml:74`).
4. **Error recovery:** macOS error cards can open microphone settings or support. Windows's error card
   currently has retry, sign-in-again and plans actions (`QuickBarView.axaml:92`); recovery is less direct.
5. **Panel composition and polish:** macOS separates the bar and secondary card and reorders them when
   growing upward (`QuickBarView.swift:24`). Windows uses one card with a fixed vertical order
   (`QuickBarView.axaml:3`). OTP countdown/auto-submit, account plan presentation and About diagnostics
   also differ. Segoe/Fluent styling is a deliberate Windows adaptation, not a matching Mac design.

macOS paths above are under `AirReply-macOS/Packages/AirReplyKit/Sources`; Windows paths are under
`AirReply-Windows/src/AirReply.App`. The fuller manual checklist remains
[QA-WINDOWS.md](../../AirReply-Windows/Docs/QA-WINDOWS.md). Real messenger insertion, microphone/privacy,
DPI/multiple monitors and actual Windows ARM64 hardware remain unverified. The unsigned installer may
trigger Windows reputation warnings. This publication makes no claim of identical behavior or complete
production readiness.
