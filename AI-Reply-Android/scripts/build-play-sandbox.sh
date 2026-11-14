#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo 'Usage: ./scripts/build-play-sandbox.sh /absolute/path/existing-upload.jks EXISTING_ALIAS' >&2
  exit 2
fi
upload_keystore="$1"
upload_alias="$2"
[[ -f "$upload_keystore" ]] || { echo 'Existing upload keystore not found.' >&2; exit 2; }
upload_keystore="$(cd "$(dirname "$upload_keystore")" && pwd)/$(basename "$upload_keystore")"
android_root="$(cd "$(dirname "$0")/.." && pwd)"
signing_jdk="${JAVA_HOME:-/Applications/Android Studio.app/Contents/jbr/Contents/Home}"
[[ -x "$signing_jdk/bin/jarsigner" && -x "$signing_jdk/bin/keytool" ]] || {
  echo 'Set JAVA_HOME to the Android Studio JDK.' >&2; exit 2;
}
cd "$android_root"

# Explicit command-line properties win over local Gradle settings. No passwords are arguments.
JAVA_HOME="$signing_jdk" ./gradlew --offline --no-daemon bundleRelease \
  -Paireply.backend=sandbox -Paireply.versionCode=5 -Paireply.versionName=1.2.1
build_config='app/build/generated/source/buildConfig/release/kz/yerek/aireply/BuildConfig.java'
grep -Fq 'BACKEND_ENVIRONMENT = "sandbox"' "$build_config"
grep -Fq 'BACKEND_BASE_URL = "https://ai-reply.kz/sandbox"' "$build_config"
output='app/build/AIReply-Sandbox-1.2.1-code5-signed.aab'
"$signing_jdk/bin/jarsigner" -keystore "$upload_keystore" -signedjar "$output" \
  app/build/outputs/bundle/release/app-release.aab "$upload_alias"
"$signing_jdk/bin/jarsigner" -verify "$output"
certificate_sha="$("$signing_jdk/bin/keytool" -J-Duser.language=en -J-Duser.country=US \
  -printcert -jarfile "$output" | awk '/SHA256:/ {print $2; exit}')"
expected_sha='55:D8:6D:5B:8C:5F:DE:F2:53:F5:B7:B2:A1:16:49:95:3D:25:8C:CD:93:4E:F1:DA:B9:97:88:72:70:F9:87:DC'
[[ "$certificate_sha" == "$expected_sha" ]] || {
  echo 'Wrong upload certificate. Do not upload this bundle to Play.' >&2; exit 1;
}
shasum -a 256 "$output"
echo "Ready for the Google Group Testers CLOSED track only: $android_root/$output"
