#!/bin/bash
# Build/test/run watcher for the Claude session (v7).
# Leave this running in Terminal. Ctrl-C to stop.
#
# It reacts only to request files in this folder and only ever runs the fixed
# commands below: xcodebuild build/test, simctl install/launch/log/screenshot/
# push for the iOS Simulator, xcodegen (if installed), Gradle build/unit
# tests/lint for AI-Reply-Android, and adb install/screenshot/tap for an
# Android EMULATOR if one is running.
# Nothing is committed, pushed, or installed on a physical device.

ROOT="$(cd "$(dirname "$0")" && pwd)"
PROJ="$ROOT/AI-Reply"
ANDROID="$ROOT/AI-Reply-Android"
cd "$PROJ" || exit 1

date '+v7 started %H:%M:%S' > "$ROOT/.watcher-v7"

# Prefer the simulator that is already booted.
pick_sim() {
  local booted
  booted="$(xcrun simctl list devices booted \
    | sed -n 's/^ *iPhone[^(]*(\([0-9A-Fa-f-]\{36\}\)) (Booted).*/\1/p' | head -1)"
  if [ -n "$booted" ]; then echo "$booted"; return; fi
  xcrun simctl list devices available \
    | sed -n 's/^ *\(iPhone [^(]*\) (\([0-9A-Fa-f-]\{36\}\)).*/\2/p' \
    | head -1
}

android_env() {
  for candidate in \
    "/Applications/Android Studio.app/Contents/jbr/Contents/Home" \
    "$HOME/Applications/Android Studio.app/Contents/jbr/Contents/Home" \
    "$(/usr/libexec/java_home -v 17 2>/dev/null)" \
    "$(/usr/libexec/java_home 2>/dev/null)"; do
    if [ -x "$candidate/bin/java" ]; then export JAVA_HOME="$candidate"; break; fi
  done
  if [ ! -f "$ANDROID/local.properties" ] && [ -d "$HOME/Library/Android/sdk" ]; then
    echo "sdk.dir=$HOME/Library/Android/sdk" > "$ANDROID/local.properties"
  fi
  ADB="$HOME/Library/Android/sdk/platform-tools/adb"
}

emulator_serial() {
  [ -x "$ADB" ] || return
  "$ADB" devices | awk '/^emulator-[0-9]+[[:space:]]+device$/ {print $1; exit}'
}

echo "Watching $PROJ   (v7)"
echo "  build   -> $ROOT/ios-build.log"
echo "  test    -> $ROOT/ios-test.log"
echo "  run     -> $ROOT/ios-run.log"
echo "  simlog  -> $ROOT/ios-simlog.txt"
echo "  android -> $ROOT/android-build.log"
echo "  simshot -> $ROOT/ios-shot.png  (also .claude-launch, .claude-simpush)"
echo "  xcodegen-> $ROOT/xcodegen.log"
echo "Close any older copy of this script first — two watchers fight over the same request files."
echo

while true; do
  if [ -f "$ROOT/.build-request" ]; then
    rm -f "$ROOT/.build-request" "$ROOT/.build-done"
    echo "$(date '+%H:%M:%S')  building..."
    xcodebuild -project AIReply.xcodeproj -scheme AIReply \
      -destination 'generic/platform=iOS Simulator' \
      -derivedDataPath build/SimDerivedData \
      CODE_SIGNING_ALLOWED=NO build > "$ROOT/ios-build.log" 2>&1
    grep -c "error:" "$ROOT/ios-build.log" > "$ROOT/.build-done"
    echo "$(date '+%H:%M:%S')  build done — $(cat "$ROOT/.build-done") error line(s)"
  fi

  # Optional first line: a test identifier for -only-testing.
  if [ -f "$ROOT/.claude-test" ]; then
    ONLY="$(head -1 "$ROOT/.claude-test" | tr -cd 'A-Za-z0-9_/')"
    rm -f "$ROOT/.claude-test" "$ROOT/.claude-test-done"
    SIM="$(pick_sim)"
    echo "$(date '+%H:%M:%S')  testing on simulator $SIM ${ONLY:+(only $ONLY)} ..."
    xcodebuild -project AIReply.xcodeproj -scheme AIReply \
      -destination "id=$SIM" \
      -derivedDataPath build/SimDerivedData \
      ${ONLY:+-only-testing:$ONLY} \
      CODE_SIGNING_ALLOWED=NO test > "$ROOT/ios-test.log" 2>&1
    echo "xcodebuild exit=$?" >> "$ROOT/ios-test.log"
    date '+%H:%M:%S' > "$ROOT/.claude-test-done"
    echo "$(date '+%H:%M:%S')  test done"
  fi

  # Ad-hoc signed build for the booted simulator (so the App Group entitlement
  # is applied), install, launch. Arguments: the app's own DEBUG switches only.
  if [ -f "$ROOT/.claude-run" ]; then
    ARGS="$(head -1 "$ROOT/.claude-run" | tr -cd 'A-Za-z0-9 _-')"
    rm -f "$ROOT/.claude-run" "$ROOT/.claude-run-done"
    SIM="$(pick_sim)"
    echo "$(date '+%H:%M:%S')  run on simulator $SIM ..."
    {
      xcrun simctl boot "$SIM" 2>/dev/null
      xcodebuild -project AIReply.xcodeproj -scheme AIReply \
        -destination "id=$SIM" \
        -derivedDataPath build/RunDerivedData \
        CODE_SIGN_IDENTITY=- CODE_SIGN_STYLE=Manual DEVELOPMENT_TEAM= PROVISIONING_PROFILE_SPECIFIER= \
        build
      echo "xcodebuild exit=$?"
      APP="build/RunDerivedData/Build/Products/Debug-iphonesimulator/AIReply.app"
      codesign -dvv --entitlements - "$APP/PlugIns/ReplyKeyboardExtension.appex" 2>&1 | tail -20
      xcrun simctl install "$SIM" "$APP" && echo "installed"
      xcrun simctl launch --terminate-running-process "$SIM" kz.yerek.replykeyboard $ARGS && echo "launched"
    } > "$ROOT/ios-run.log" 2>&1
    date '+%H:%M:%S' > "$ROOT/.claude-run-done"
    echo "$(date '+%H:%M:%S')  run done"
  fi

  # Recent simulator logs for our two processes, plus their newest crash reports.
  if [ -f "$ROOT/.claude-simlog" ]; then
    rm -f "$ROOT/.claude-simlog" "$ROOT/.claude-simlog-done"
    SIM="$(pick_sim)"
    echo "$(date '+%H:%M:%S')  collecting simulator logs ..."
    {
      echo "=== log show (last 4m) ==="
      xcrun simctl spawn "$SIM" log show --last 4m --style compact \
        --predicate 'process CONTAINS "ReplyKeyboard" OR process == "AIReply" OR (eventMessage CONTAINS[c] "replykeyboard")' 2>&1 | tail -400
      echo
      echo "=== crash reports ==="
      for f in $(ls -t "$HOME/Library/Logs/DiagnosticReports/"*ReplyKeyboard* "$HOME/Library/Logs/DiagnosticReports/"AIReply* 2>/dev/null | head -2); do
        echo "--- $f"; head -120 "$f"
      done
    } > "$ROOT/ios-simlog.txt" 2>&1
    date '+%H:%M:%S' > "$ROOT/.claude-simlog-done"
    echo "$(date '+%H:%M:%S')  simlog done"
  fi

  # Android: debug build + JVM unit tests.
  if [ -f "$ROOT/.claude-android" ]; then
    EXTRA=""
    [ "$(head -1 "$ROOT/.claude-android" | tr -cd 'a-z')" = "lint" ] && EXTRA="lintDebug"
    rm -f "$ROOT/.claude-android" "$ROOT/.claude-android-done"
    echo "$(date '+%H:%M:%S')  android build + unit tests ..."
    android_env
    (
      cd "$ANDROID" || exit 1
      echo "JAVA_HOME=$JAVA_HOME"
      chmod +x gradlew
      ./gradlew --no-daemon assembleDebug testDebugUnitTest $EXTRA 2>&1
      echo "GRADLE_EXIT=$?"
    ) > "$ROOT/android-build.log" 2>&1
    date '+%H:%M:%S' > "$ROOT/.claude-android-done"
    echo "$(date '+%H:%M:%S')  android done"
  fi

  # Android EMULATOR only: install the debug APK and take a screenshot, or send
  # one sanitized input (tap X Y | swipe X1 Y1 X2 Y2 MS | text ascii | key NAME).
  if [ -f "$ROOT/.claude-emu" ]; then
    CMD="$(head -1 "$ROOT/.claude-emu")"
    rm -f "$ROOT/.claude-emu" "$ROOT/.claude-emu-done"
    android_env
    SERIAL="$(emulator_serial)"
    {
      if [ -z "$SERIAL" ]; then
        echo "no running emulator"
      else
        set -- $CMD
        case "$1" in
          install)
            "$ADB" -s "$SERIAL" install -r "$ANDROID/app/build/outputs/apk/debug/app-debug.apk"
            "$ADB" -s "$SERIAL" shell am start -n kz.yerek.aireply/.MainActivity ;;
          tap)   "$ADB" -s "$SERIAL" shell input tap "${2//[^0-9]/}" "${3//[^0-9]/}" ;;
          swipe) "$ADB" -s "$SERIAL" shell input swipe "${2//[^0-9]/}" "${3//[^0-9]/}" "${4//[^0-9]/}" "${5//[^0-9]/}" "${6//[^0-9]/}" ;;
          text)  "$ADB" -s "$SERIAL" shell input text "${2//[^A-Za-z0-9]/}" ;;
          key)   "$ADB" -s "$SERIAL" shell input keyevent "${2//[^A-Z_0-9]/}" ;;
          shade) "$ADB" -s "$SERIAL" shell cmd statusbar expand-notifications ;;
          fresh)
            "$ADB" -s "$SERIAL" uninstall kz.yerek.aireply
            "$ADB" -s "$SERIAL" install "$ANDROID/app/build/outputs/apk/debug/app-debug.apk"
            "$ADB" -s "$SERIAL" shell am start -n kz.yerek.aireply/.MainActivity ;;
          notif-reset)
            "$ADB" -s "$SERIAL" shell pm revoke kz.yerek.aireply android.permission.POST_NOTIFICATIONS
            "$ADB" -s "$SERIAL" shell pm clear-permission-flags kz.yerek.aireply android.permission.POST_NOTIFICATIONS user-set user-fixed ;;
        esac
        sleep 1
        "$ADB" -s "$SERIAL" exec-out screencap -p > "$ROOT/android-shot.png" && echo "screenshot saved"
      fi
    } > "$ROOT/android-emu.log" 2>&1
    date '+%H:%M:%S' > "$ROOT/.claude-emu-done"
  fi

  # iOS Simulator screenshot.
  if [ -f "$ROOT/.claude-simshot" ]; then
    rm -f "$ROOT/.claude-simshot" "$ROOT/.claude-simshot-done"
    SIM="$(pick_sim)"
    xcrun simctl io "$SIM" screenshot "$ROOT/ios-shot.png" > "$ROOT/ios-shot.log" 2>&1
    date '+%H:%M:%S' > "$ROOT/.claude-simshot-done"
  fi

  # Relaunch the installed app with its own DEBUG switches (no rebuild), then a screenshot.
  if [ -f "$ROOT/.claude-launch" ]; then
    ARGS="$(head -1 "$ROOT/.claude-launch" | tr -cd 'A-Za-z0-9 _-')"
    rm -f "$ROOT/.claude-launch" "$ROOT/.claude-launch-done"
    SIM="$(pick_sim)"
    {
      xcrun simctl launch --terminate-running-process "$SIM" kz.yerek.replykeyboard $ARGS && echo "launched"
      sleep 4
      xcrun simctl io "$SIM" screenshot "$ROOT/ios-shot.png"
    } > "$ROOT/ios-launch.log" 2>&1
    date '+%H:%M:%S' > "$ROOT/.claude-launch-done"
  fi

  # Simulated remote notification for this app only. The request file is the APNs JSON payload.
  if [ -f "$ROOT/.claude-simpush" ]; then
    mv -f "$ROOT/.claude-simpush" "$ROOT/.claude-simpush.json"
    rm -f "$ROOT/.claude-simpush-done"
    SIM="$(pick_sim)"
    {
      xcrun simctl push "$SIM" kz.yerek.replykeyboard "$ROOT/.claude-simpush.json" && echo "pushed"
      sleep 3
      xcrun simctl io "$SIM" screenshot "$ROOT/ios-shot.png"
    } > "$ROOT/ios-push.log" 2>&1
    date '+%H:%M:%S' > "$ROOT/.claude-simpush-done"
  fi

  # Regenerate AIReply.xcodeproj from project.yml, only if xcodegen is installed.
  if [ -f "$ROOT/.claude-xcodegen" ]; then
    rm -f "$ROOT/.claude-xcodegen" "$ROOT/.claude-xcodegen-done"
    {
      if command -v xcodegen >/dev/null 2>&1; then
        xcodegen generate --spec project.yml; echo "exit=$?"
      else
        echo "xcodegen not installed"
      fi
    } > "$ROOT/xcodegen.log" 2>&1
    date '+%H:%M:%S' > "$ROOT/.claude-xcodegen-done"
  fi

  sleep 2
done
