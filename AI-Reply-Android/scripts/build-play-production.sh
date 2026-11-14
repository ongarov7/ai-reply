#!/usr/bin/env bash
set -euo pipefail

unsigned_only=false
if [[ $# -eq 1 && "$1" == '--unsigned-only' ]]; then
  unsigned_only=true
elif [[ $# -eq 2 ]]; then
  upload_keystore="$1"
  upload_alias="$2"
  [[ -f "$upload_keystore" ]] || { echo 'Existing upload keystore not found.' >&2; exit 2; }
  upload_keystore="$(cd "$(dirname "$upload_keystore")" && pwd)/$(basename "$upload_keystore")"
  if [[ "$upload_alias" == 'androiddebugkey' || "$(basename "$upload_keystore")" == 'debug.keystore' ]]; then
    echo 'Use the existing Play upload key, not an Android debug key.' >&2
    exit 2
  fi
else
  echo 'Usage: ./scripts/build-play-production.sh --unsigned-only' >&2
  echo '   or: ./scripts/build-play-production.sh /absolute/path/existing-upload.jks EXISTING_ALIAS' >&2
  exit 2
fi

android_root="$(cd "$(dirname "$0")/.." && pwd)"
signing_jdk="${JAVA_HOME:-/Applications/Android Studio.app/Contents/jbr/Contents/Home}"
[[ -x "$signing_jdk/bin/java" && -x "$signing_jdk/bin/jarsigner" && -x "$signing_jdk/bin/keytool" ]] || {
  echo 'Set JAVA_HOME to the Android Studio JDK.' >&2; exit 2;
}
cd "$android_root"
output_dir="$android_root/app/build/play-production"
mkdir -p "$output_dir"
unsigned_output="$output_dir/AIReply-1.2.2-code6-production-unsigned.aab"
signed_output="$output_dir/AIReply-1.2.2-code6-production-signed.aab"
if [[ "$unsigned_only" == false && -e "$signed_output" ]]; then
  echo 'The signed production output already exists. Review it before building again.' >&2
  exit 2
fi
init_script="$(mktemp "$output_dir/.output-init.XXXXXX")"
temporary_signed=''
trap 'rm -f "$init_script"; if [[ -n "$temporary_signed" ]]; then rm -f "$temporary_signed"; fi' EXIT
cat > "$init_script" <<'GRADLE'
// Relocate only the final bundle; preserve every earlier production/sandbox AAB.
gradle.projectsEvaluated {
    def appProject = gradle.rootProject.findProject(":app")
    appProject.tasks.named("signReleaseBundle").configure {
        finalBundleFile.set(new File(appProject.layout.buildDirectory.get().asFile,
                "play-production/AIReply-1.2.2-code6-production-unsigned.aab"))
    }
}
GRADLE

# Explicit properties override local settings. No signing passwords are command arguments.
JAVA_HOME="$signing_jdk" ./gradlew --offline --no-daemon --no-configuration-cache bundleRelease \
  -Paireply.backend=production -Paireply.versionCode=6 -Paireply.versionName=1.2.2 \
  --init-script "$init_script"
build_config='app/build/generated/source/buildConfig/release/kz/yerek/aireply/BuildConfig.java'
grep -Fq 'BACKEND_ENVIRONMENT = "production"' "$build_config"
grep -Fq 'BACKEND_BASE_URL = "https://ai-reply.kz"' "$build_config"
grep -Fq 'VERSION_CODE = 6;' "$build_config"
grep -Fq 'VERSION_NAME = "1.2.2"' "$build_config"
[[ -f "$unsigned_output" ]] || { echo 'Production AAB was not produced.' >&2; exit 1; }
shasum -a 256 "$unsigned_output" > "$unsigned_output.sha256"
if [[ "$unsigned_only" == true ]]; then
  cat "$unsigned_output.sha256"
  echo "Unsigned production bundle for review; sign with the existing upload key: $unsigned_output"
  exit 0
fi

# Publish a final signing output only after its certificate matches the approved upload key.
temporary_signed="$(mktemp "$output_dir/.signing.XXXXXX")"
"$signing_jdk/bin/jarsigner" -keystore "$upload_keystore" -signedjar "$temporary_signed" \
  "$unsigned_output" "$upload_alias"
"$signing_jdk/bin/jarsigner" -verify "$temporary_signed"
certificate_sha="$("$signing_jdk/bin/keytool" -J-Duser.language=en -J-Duser.country=US \
  -printcert -jarfile "$temporary_signed" | awk '/SHA256:/ {print $2; exit}')"
expected_sha='55:D8:6D:5B:8C:5F:DE:F2:53:F5:B7:B2:A1:16:49:95:3D:25:8C:CD:93:4E:F1:DA:B9:97:88:72:70:F9:87:DC'
[[ "$certificate_sha" == "$expected_sha" ]] || {
  echo 'Wrong upload certificate. No signed release was retained.' >&2; exit 1;
}
mv "$temporary_signed" "$signed_output"
temporary_signed=''
shasum -a 256 "$signed_output" | tee "$signed_output.sha256"
echo "Ready for review in the PUBLIC PRODUCTION track: $signed_output"
echo 'This helper does not upload or publish anything.'
