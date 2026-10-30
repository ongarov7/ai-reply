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
IDENTITY="Developer ID Application"
WORK=build/release
OUT=dist

if ! security find-identity -v -p codesigning | grep -q "$IDENTITY"; then
    echo "error: no \"$IDENTITY\" certificate in the keychain" >&2
    exit 1
fi

settings=$(xcodebuild -project AirReply.xcodeproj -scheme AirReply -configuration Release -showBuildSettings 2>/dev/null)
VERSION=$(awk -F' = ' '/ MARKETING_VERSION = /{print $2; exit}' <<<"$settings")
BUILD=$(awk -F' = ' '/ CURRENT_PROJECT_VERSION = /{print $2; exit}' <<<"$settings")
MIN_OS=$(awk -F' = ' '/ MACOSX_DEPLOYMENT_TARGET = /{print $2; exit}' <<<"$settings")

rm -rf "$WORK"
mkdir -p "$WORK" "$OUT"

xcodebuild -project AirReply.xcodeproj -scheme AirReply -configuration Release \
    -destination 'generic/platform=macOS' -archivePath "$WORK/AirReply.xcarchive" archive
xcodebuild -exportArchive -archivePath "$WORK/AirReply.xcarchive" \
    -exportOptionsPlist scripts/ExportOptions.plist -exportPath "$WORK/export"

APP="$WORK/export/AI Reply.app"
codesign --verify --deep --strict --verbose=2 "$APP"
lipo -archs "$APP/Contents/MacOS/AI Reply"

# The app itself is notarized and stapled first, so it stays valid when copied out of the DMG.
ditto -c -k --keepParent "$APP" "$WORK/AI-Reply.zip"
xcrun notarytool submit "$WORK/AI-Reply.zip" --keychain-profile "$PROFILE" --wait
xcrun stapler staple "$APP"

STAGE="$WORK/dmg"
mkdir -p "$STAGE"
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
DMG="$OUT/AI-Reply-$VERSION.dmg"
rm -f "$DMG"
hdiutil create -volname "AI Reply" -srcfolder "$STAGE" -fs HFS+ -format UDZO -ov "$DMG"
codesign --sign "$IDENTITY" --timestamp "$DMG"
xcrun notarytool submit "$DMG" --keychain-profile "$PROFILE" --wait
xcrun stapler staple "$DMG"
spctl --assess --type open --context context:primary-signature --verbose=2 "$DMG"

SHA=$(shasum -a 256 "$DMG" | awk '{print $1}')
SIZE=$(stat -f %z "$DMG")
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
