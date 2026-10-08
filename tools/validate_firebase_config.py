#!/usr/bin/env python3
"""Validate the shared Firebase client configs without printing API keys."""

import argparse
import json
import plistlib
import re
import sys
from pathlib import Path


PROJECT_ID = "ai-reply-4bf8f"
SENDER_ID = "307300959376"
ANDROID_PACKAGE = "kz.yerek.aireply"
IOS_BUNDLE = "kz.ai-reply.reply.keyboard.keyboard"


def require(condition, message):
    if not condition:
        raise ValueError(message)


def client_only(data):
    if isinstance(data, dict):
        require(data.get("type") != "service_account", "A service account must never be used as a client config")
        require(not {"private_key", "private_key_id", "client_secret"}.intersection(data),
                "Private credentials found in a client config")
        for value in data.values():
            client_only(value)
    elif isinstance(data, list):
        for value in data:
            client_only(value)


def validate(root):
    android_dir = root / "AI-Reply-Android/app"
    ios_dir = root / "AI-Reply"
    android = json.loads((android_dir / "google-services.json").read_text())
    with (ios_dir / "Config/Firebase/GoogleService-Info.plist").open("rb") as file:
        ios = plistlib.load(file)
    client_only(android)
    client_only(ios)

    gradle = (android_dir / "build.gradle.kts").read_text()
    xcodegen = (ios_dir / "project.yml").read_text()
    xcode = (ios_dir / "AIReply.xcodeproj/project.pbxproj").read_text()
    require(re.search(r'applicationId\s*=\s*"' + re.escape(ANDROID_PACKAGE) + '"', gradle),
            "Android applicationId differs from the Firebase registration")
    require("com.google.gms.google-services" in gradle, "Android Google Services plugin is missing")
    require("libs.firebase.messaging" in gradle, "Android Firebase Messaging dependency is missing")
    require("PRODUCT_BUNDLE_IDENTIFIER: " + IOS_BUNDLE in xcodegen,
            "iOS project.yml bundle ID differs from the Firebase registration")
    require('PRODUCT_BUNDLE_IDENTIFIER = "' + IOS_BUNDLE + '";' in xcode,
            "iOS Xcode project bundle ID differs from the Firebase registration")
    require("Firebase config" in xcode and "Config/Firebase/GoogleService-Info.plist" in xcode,
            "iOS Firebase config copy phase is missing")

    info = android["project_info"]
    require(info["project_id"] == PROJECT_ID, "Android config belongs to a different Firebase project")
    require(str(info["project_number"]) == SENDER_ID, "Android sender ID differs from the project")
    clients = [client for client in android["client"]
               if client["client_info"].get("android_client_info", {}).get("package_name") == ANDROID_PACKAGE]
    require(len(clients) == 1, "Android config must have exactly one matching client")
    android_app_id = clients[0]["client_info"]["mobilesdk_app_id"]
    require(android_app_id.startswith("1:" + SENDER_ID + ":android:"), "Invalid Android Firebase app ID")
    require(any(key.get("current_key") for key in clients[0].get("api_key", [])), "Android API key is missing")

    require(ios["PROJECT_ID"] == PROJECT_ID, "iOS config belongs to a different Firebase project")
    require(ios["BUNDLE_ID"] == IOS_BUNDLE, "iOS config bundle ID does not match the application")
    require(str(ios["GCM_SENDER_ID"]) == SENDER_ID, "iOS sender ID differs from the project")
    require(ios["GOOGLE_APP_ID"].startswith("1:" + SENDER_ID + ":ios:"), "Invalid iOS Firebase app ID")
    require(bool(ios["API_KEY"]), "iOS API key is missing")
    require(ios.get("IS_GCM_ENABLED") is True, "Messaging is disabled in the iOS config")
    require(ios.get("IS_ANALYTICS_ENABLED") is False, "Analytics must stay disabled for this setup")

    print("Firebase client configs: OK")
    print("Project: " + PROJECT_ID + " (sender " + SENDER_ID + ")")
    print("Android: " + ANDROID_PACKAGE + " / " + android_app_id)
    print("iOS: " + IOS_BUNDLE + " / " + ios["GOOGLE_APP_ID"])
    print("This validates local configuration; APNs, backend credentials and device delivery need separate checks.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    try:
        validate(args.root)
    except (OSError, ValueError, KeyError, TypeError, plistlib.InvalidFileException) as error:
        print("Firebase config validation failed: " + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
