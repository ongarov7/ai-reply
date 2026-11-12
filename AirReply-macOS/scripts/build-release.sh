#!/bin/bash
# Builds the signed, notarized download for the website:
#   dist/AI-Reply-<version>.dmg, its SHA-256 and a manifest entry.
#
# Needs on this Mac (see README "Release"):
#   - a "Developer ID Application" certificate of team 2PK6339Q47 in the login keychain;
#   - a notarytool keychain profile (default name: airreply-notary).
# Uploads go only to Apple's notary service. Nothing is published.
set -euo pipefail

cd "$(dirname "$0")/.."
PROFILE="${NOTARY_PROFILE:-airreply-notary}"
TEAM_ID=2PK6339Q47
IDENTITY=$(security find-identity -v -p codesigning | \
    awk -v team="$TEAM_ID" '/"Developer ID Application: / && $0 ~ ("\\(" team "\\)\"$") {print $2; exit}')
WORK=build/release
OUT=dist

if [ -z "$IDENTITY" ]; then
    echo "error: no Developer ID Application certificate with a private key for team $TEAM_ID in the keychain" >&2
    exit 1
fi
if ! xcrun notarytool history --keychain-profile "$PROFILE" >/dev/null; then
    echo "error: configure the notarytool keychain profile '$PROFILE' before building (see README)" >&2
    exit 1
fi

settings=$(xcodebuild -project AirReply.xcodeproj -scheme AirReply -configuration Release -showBuildSettings 2>/dev/null)
VERSION=$(awk -F' = ' '/ MARKETING_VERSION = /{print $2; exit}' <<<"$settings")
BUILD=$(awk -F' = ' '/ CURRENT_PROJECT_VERSION = /{print $2; exit}' <<<"$settings")
MIN_OS=$(awk -F' = ' '/ MACOSX_DEPLOYMENT_TARGET = /{print $2; exit}' <<<"$settings")

rm -rf "$WORK"
mkdir -p "$WORK" "$OUT"

xcodebuild -project AirReply.xcodeproj -scheme AirReply -configuration Release \
    -destination 'generic/platform=macOS' -archivePath "$WORK/AirReply.xcarchive" \
    CODE_SIGN_IDENTITY="$IDENTITY" archive
xcodebuild -exportArchive -archivePath "$WORK/AirReply.xcarchive" \
    -exportOptionsPlist scripts/ExportOptions.plist -exportPath "$WORK/export"

APP="$WORK/export/AI Reply.app"
codesign --verify --deep --strict --verbose=2 "$APP"
lipo "$APP/Contents/MacOS/AI Reply" -verify_arch arm64 x86_64
signing_info=$(codesign -d --verbose=4 "$APP" 2>&1)
grep -Fx "TeamIdentifier=$TEAM_ID" <<<"$signing_info" >/dev/null
/usr/libexec/PlistBuddy -c 'Print :AIReplyBackendBaseURL' "$APP/Contents/Info.plist" | grep -Fx 'https://ai-reply.kz' >/dev/null

notarize() {
    local file="$1" result="$2"
    xcrun notarytool submit "$file" --keychain-profile "$PROFILE" --wait --output-format json > "$result"
    python3 - "$result" <<'PY'
import json, sys
result = json.load(open(sys.argv[1]))
print('Notarization:', result.get('id'), result.get('status'))
if result.get('status') != 'Accepted':
    sys.exit('error: Apple did not accept this submission; inspect the notarytool log before publishing')
PY
}

# The app itself is notarized and stapled first, so it stays valid when copied out of the DMG.
ditto -c -k --keepParent "$APP" "$WORK/AI-Reply.zip"
notarize "$WORK/AI-Reply.zip" "$WORK/app-notarization.json"
xcrun stapler staple "$APP"
xcrun stapler validate "$APP"
spctl --assess --type execute --verbose=2 "$APP"

STAGE="$WORK/dmg"
mkdir -p "$STAGE"
ditto "$APP" "$STAGE/AI Reply.app"
ln -s /Applications "$STAGE/Applications"
DMG="$OUT/AI-Reply-$VERSION.dmg"
rm -f "$DMG"
hdiutil create -volname "AI Reply" -srcfolder "$STAGE" -fs HFS+ -format UDZO -ov "$DMG"
codesign --sign "$IDENTITY" --timestamp "$DMG"
notarize "$DMG" "$WORK/dmg-notarization.json"
xcrun stapler staple "$DMG"
xcrun stapler validate "$DMG"
spctl --assess --type open --context context:primary-signature --verbose=2 "$DMG"

SHA=$(shasum -a 256 "$DMG" | awk '{print $1}')
SIZE=$(stat -f %z "$DMG")
(cd "$OUT" && shasum -a 256 "$(basename "$DMG")" > "$(basename "$DMG").sha256")
cat > "$OUT/macos-$VERSION.json" <<JSON
{
  "platform": "macos",
  "version": "$VERSION",
  "build": "$BUILD",
  "min_os": "$MIN_OS",
  "file": "$(basename "$DMG")",
  "size": $SIZE,
  "sha256": "$SHA"
}
JSON

echo "ready: $DMG"
echo "sha256: $SHA"
