#!/usr/bin/env python3
"""Prepare isolated billing sandbox settings without printing credentials."""

import argparse
from datetime import datetime, timezone
import os
from pathlib import Path
import re
import secrets
import tempfile


ASSIGNMENT = re.compile(r"^\s*(?:export\s+)?([A-Z][A-Z0-9_]*)\s*=\s*(.*?)\s*$")


def configure(target: Path, enable_bcc: bool = False):
    target = target.resolve(strict=True)
    original = target.stat()
    content = target.read_text(encoding="utf-8")
    existing = {}
    for line in content.splitlines():
        match = ASSIGNMENT.match(line)
        if match:
            existing[match[1]] = match[2].strip("\"'")

    values = {
        "BILLING_SANDBOX_ENABLED": "true",
        "BILLING_SANDBOX_BCC_ENABLED": "true" if enable_bcc else "false",
        "BCC_CHECKOUT_ENABLED": "false",
    }
    defaults = {
        "BCC_SANDBOX_MERCHANT_ID": "00000001",
        "BCC_SANDBOX_MERCHANT_NAME": '"TOO MERCHANT"',
        "BCC_SANDBOX_TERMINAL_ID": "88888881",
        # Public test key from BCC's published Postman collection.
        "BCC_SANDBOX_MAC_KEY": "6BB0AC02E47BDF73D98FEB777F3B5294",
        "BCC_CHECKOUT_NOTIFY_USERNAME": "aireply_sandbox",
    }
    for key, value in defaults.items():
        if not existing.get(key):
            values[key] = value
    for key in ("BCC_CHECKOUT_NOTIFY_PASSWORD", "BCC_CHECKOUT_STATE_SECRET"):
        if not existing.get(key):
            values[key] = secrets.token_hex(32)
        elif len(existing[key]) < 32:
            raise ValueError(f"{key} must be at least 32 characters; existing value was preserved")
    if existing.get("APP_ENV", "development").lower() == "production":
        values["APPLE_IAP_ACCEPT_SANDBOX"] = "false"

    lines = []
    for line in content.splitlines(keepends=True):
        match = ASSIGNMENT.match(line.rstrip("\r\n"))
        if not match or match[1] not in values:
            lines.append(line)
    updated = "".join(lines)
    if updated and not updated.endswith("\n"):
        updated += "\n"
    updated += "".join(f"{key}={value}\n" for key, value in values.items())

    backup = target.with_name(target.name + ".billing-backup-" + datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S%fZ"))
    fd = os.open(backup, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as output:
        output.write(content)
        output.flush()
        os.fsync(output.fileno())

    temporary = None
    try:
        fd, temporary = tempfile.mkstemp(prefix=".env.billing-", dir=target.parent)
        with os.fdopen(fd, "w", encoding="utf-8", newline="") as output:
            os.fchmod(output.fileno(), 0o600)
            current = os.fstat(output.fileno())
            if (current.st_uid, current.st_gid) != (original.st_uid, original.st_gid):
                os.fchown(output.fileno(), original.st_uid, original.st_gid)
            output.write(updated)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, target)
        temporary = None
    finally:
        if temporary is not None:
            os.unlink(temporary)
    return backup


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", type=Path, default=Path(".env"))
    parser.add_argument("--enable-bcc", action="store_true", help="use only after configuring the same notification Basic Auth at BCC")
    args = parser.parse_args()
    try:
        configure(args.env, args.enable_bcc)
    except (OSError, ValueError) as error:
        parser.exit(1, f"Sandbox settings were not replaced: {error}\n")
    print("Sandbox settings saved with a private backup; credentials are not printed.")
    print("Primary JWT keys and production database settings are unchanged.")
    print("Next: docker compose build backend && docker compose run --rm --no-deps backend -check")
    if not args.enable_bcc:
        print("BCC checkout remains off until notification Basic Auth is configured at the bank.")


if __name__ == "__main__":
    main()
