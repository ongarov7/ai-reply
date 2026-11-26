#!/usr/bin/env python3
"""Verify the public campaign redirect or preflight it against an HTTP origin."""

import argparse
from dataclasses import dataclass
import http.client
import sys
from urllib.parse import parse_qs, urlencode, urlsplit


CAMPAIGN_URL = "https://target.ai-reply.kz/"
APP_STORE = "https://apps.apple.com/kz/app/ai-reply/id6818570150"
GOOGLE_PLAY = "https://play.google.com/store/apps/details?id=kz.yerek.aireply"
WEBSITE = "https://ai-reply.kz/"

IPHONE = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15"
IPAD = "Mozilla/5.0 (iPad; CPU OS 18_6 like Mac OS X) AppleWebKit/605.1.15"
MAC = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15"
ANDROID = "Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko)"
WINDOWS = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/132.0.0.0 Safari/537.36"
LINUX = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/132.0.0.0 Safari/537.36"
META_PREVIEW = "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)"

UTM = {
    "utm_source": "smoke_meta",
    "utm_medium": "paid_social",
    "utm_campaign": "Smoke + тест",
    "utm_term": "ai reply",
    "utm_content": "video & still",
    "utm_id": "smoke",
    "utm_source_platform": "Meta",
    "utm_creative_format": "video",
    "utm_marketing_tactic": "prospecting",
}


@dataclass(frozen=True)
class Case:
    name: str
    agent: str
    destination: str
    method: str = "GET"
    headers: tuple = ()
    query: str = ""
    destination_query: str = ""


CASES = (
    Case("iPhone Safari", f"{IPHONE} Version/18.6 Mobile/15E148 Safari/604.1", APP_STORE),
    Case("iPhone Chrome", f"{IPHONE} CriOS/132.0.0.0 Mobile/15E148 Safari/604.1", APP_STORE),
    Case("iPad mobile mode", f"{IPAD} Version/18.6 Mobile/15E148 Safari/604.1", APP_STORE),
    Case("iPad with desktop and mobile tokens", f"{MAC} Version/18.6 Mobile/15E148 Safari/604.1", APP_STORE),
    Case("Instagram on iOS", f"{IPHONE} Mobile/15E148 Instagram 370.0.0", APP_STORE),
    Case("Facebook on iOS", f"{IPHONE} Mobile/15E148 [FBAN/FBIOS;FBAV/510.0.0.0]", APP_STORE),
    Case("Telegram on iOS", f"{IPHONE} Mobile/15E148 Telegram/11.2", APP_STORE),
    Case("WhatsApp on iOS", f"{IPHONE} Mobile/15E148 WhatsApp/2.25.4", APP_STORE),
    Case("Android Chrome", f"{ANDROID} Chrome/132.0.0.0 Mobile Safari/537.36", GOOGLE_PLAY),
    Case("Instagram on Android", f"{ANDROID} Chrome/132.0.0.0 Mobile Safari/537.36 Instagram 370.0.0", GOOGLE_PLAY),
    Case("Facebook on Android", f"{ANDROID} Version/4.0 Chrome/132.0.0.0 Mobile Safari/537.36 [FB_IAB/FB4A;FBAV/510.0.0.0]", GOOGLE_PLAY),
    Case("Telegram on Android", f"{ANDROID} Chrome/132.0.0.0 Mobile Safari/537.36 Telegram-Android/11.2", GOOGLE_PLAY),
    Case("WhatsApp on Android", f"{ANDROID} Chrome/132.0.0.0 Mobile Safari/537.36 WhatsApp/2.25.4", GOOGLE_PLAY),
    Case("macOS Safari or fully masked iPad", f"{MAC} Version/18.6 Safari/605.1.15", WEBSITE),
    Case("Windows Chrome", WINDOWS, WEBSITE),
    Case("Linux Chrome", LINUX, WEBSITE),
    Case("Android client hint in desktop mode", LINUX, GOOGLE_PLAY, headers=(("Sec-CH-UA-Platform", '"Android"'),)),
    Case("iPadOS client hint in desktop mode", MAC, APP_STORE, headers=(("Sec-CH-UA-Platform", '"iPadOS"'),)),
    Case("Unknown device", "CampaignCheck/1.0", WEBSITE),
    Case("Missing User-Agent", "", WEBSITE),
    Case("Meta link preview", META_PREVIEW, WEBSITE, headers=(("Range", "bytes=0-524288"), ("Accept-Encoding", "gzip, deflate"))),
    Case("Meta ads crawler", "meta-externalads/1.1", WEBSITE),
    Case("Meta ads crawler with Android tokens", f"{ANDROID} meta-externalads/1.1", WEBSITE),
    Case("Telegram preview", "TelegramBot (like TwitterBot)", WEBSITE),
    Case("WhatsApp preview", "WhatsApp/2.25.4", WEBSITE),
    Case("iPhone HEAD", IPHONE, APP_STORE, method="HEAD"),
    Case("Meta HEAD", META_PREVIEW, WEBSITE, method="HEAD"),
    Case("Desktop UTM forwarding", WINDOWS, WEBSITE, query=urlencode(UTM), destination_query=urlencode(UTM)),
    Case("iOS UTM forwarding", IPHONE, APP_STORE, query=urlencode(UTM), destination_query=urlencode(UTM)),
    Case("Android UTM forwarding", ANDROID, GOOGLE_PLAY, query=urlencode(UTM), destination_query=urlencode(UTM)),
    Case("Unrecognized redirect parameters", WINDOWS, WEBSITE, query=urlencode({"url": "https://example.invalid/", "next": CAMPAIGN_URL, "fbclid": "test"})),
    Case("Android package identifier is fixed", ANDROID, GOOGLE_PLAY, query=urlencode({"id": "example.invalid"})),
    Case("Unsafe UTM values are dropped", IPHONE, APP_STORE, query=urlencode({"utm_source": "meta\r\nLocation: https://example.invalid", "utm_campaign": "x" * 257})),
    Case("Duplicate UTM keys use the first value", ANDROID, GOOGLE_PLAY, query="utm_source=meta&utm_source=other", destination_query="utm_source=meta"),
)


def check_response(connection, host, path, case):
    headers = dict(case.headers)
    headers["Host"] = host
    if case.agent:
        headers["User-Agent"] = case.agent
    query = f"?{case.query}" if case.query else ""
    connection.request(case.method, f"{path}{query}", headers=headers)
    with connection.getresponse() as response:
        response.read()
        if response.status != 302:
            return f"expected HTTP 302, got {response.status}"
        cache_control = response.getheader("Cache-Control", "").lower()
        if "no-store" not in {directive.strip() for directive in cache_control.split(",")}:
            return f"expected Cache-Control no-store, got {cache_control!r}"
        location = response.getheader("Location", "")

    actual = urlsplit(location)
    expected = urlsplit(case.destination)
    if (actual.scheme, actual.netloc, actual.path or "/", actual.fragment) != (
        expected.scheme, expected.netloc, expected.path or "/", ""
    ):
        return f"expected destination {case.destination}, got {location!r}"

    expected_query = parse_qs("&".join(filter(None, (expected.query, case.destination_query))))
    actual_query = parse_qs(actual.query, keep_blank_values=True)
    if actual_query != expected_query:
        return f"expected query {expected_query!r}, got {actual_query!r}"
    return None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default=CAMPAIGN_URL, help="campaign HTTPS URL")
    parser.add_argument("--origin", metavar="IP[:PORT]", help="connect directly over HTTP and retain the campaign Host header")
    args = parser.parse_args()

    try:
        target = urlsplit(args.url)
        target_port = target.port
        if target.scheme != "https" or not target.hostname or target.username is not None or target.query or target.fragment:
            parser.error("--url must be an HTTPS URL without credentials, query, or fragment")
        if args.origin:
            origin = urlsplit(f"//{args.origin}")
            if not origin.hostname or origin.username is not None or origin.path or origin.query or origin.fragment:
                parser.error("--origin must be an address in IP[:PORT] form")
            connection = http.client.HTTPConnection(origin.hostname, origin.port or 80, timeout=8)
        else:
            connection = http.client.HTTPSConnection(target.hostname, target_port or 443, timeout=8)
    except ValueError as error:
        parser.error(str(error))

    failures = 0
    try:
        for case in CASES:
            error = check_response(connection, target.netloc, target.path or "/", case)
            if error:
                failures += 1
                print(f"FAIL {case.name} — {error}", file=sys.stderr)
            else:
                print(f"PASS {case.name}")
    except (OSError, http.client.HTTPException, ValueError) as error:
        print(f"FAIL {case.name} — {error}", file=sys.stderr)
        return 1
    finally:
        connection.close()

    print(f"{len(CASES) - failures}/{len(CASES)} campaign redirect checks passed for {args.url}")
    return int(bool(failures))


if __name__ == "__main__":
    sys.exit(main())
