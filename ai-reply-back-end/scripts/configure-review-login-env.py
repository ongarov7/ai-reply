#!/usr/bin/env python3
"""Configure one store-review login in an existing .env without logging values."""

import argparse
import os
from pathlib import Path
import re
import tempfile


def email(value):
    value = value.strip().lower()
    if not re.fullmatch(r"[a-z0-9._+-]+@[a-z0-9-]+(?:\.[a-z0-9-]+)+", value):
        raise argparse.ArgumentTypeError("use one plain e-mail address")
    return value


def code(value):
    if not re.fullmatch(r"[0-9]{4}", value):
        raise argparse.ArgumentTypeError("use exactly four digits")
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", type=Path, default=Path(".env"))
    parser.add_argument("--email", type=email)
    parser.add_argument("--code", type=code)
    parser.add_argument("--clear", action="store_true")
    args = parser.parse_args()
    if args.clear:
        if args.email is not None or args.code is not None:
            parser.error("--clear cannot be combined with --email or --code")
    elif args.email is None or args.code is None:
        parser.error("--email and --code must be supplied together")

    values = {
        "REVIEW_LOGIN_EMAIL": "" if args.clear else args.email,
        "REVIEW_LOGIN_CODE": "" if args.clear else args.code,
    }
    temporary = None
    try:
        target = args.env.resolve(strict=True)
        original = target.stat()
        # Remove duplicate assignments, including optional shell-style export.
        assignment = re.compile(r"^\s*(?:export\s+)?(REVIEW_LOGIN_EMAIL|REVIEW_LOGIN_CODE)\s*=")
        lines = target.read_text(encoding="utf-8").splitlines(keepends=True)
        content = "".join(line for line in lines if not assignment.match(line))
        if content and not content.endswith("\n"):
            content += "\n"
        content += "".join(f"{name}={value}\n" for name, value in values.items())
        fd, temporary = tempfile.mkstemp(prefix=".env.review-", dir=target.parent)
        with os.fdopen(fd, "w", encoding="utf-8", newline="") as output:
            os.fchmod(output.fileno(), 0o600)
            current = os.fstat(output.fileno())
            if (current.st_uid, current.st_gid) != (original.st_uid, original.st_gid):
                os.fchown(output.fileno(), original.st_uid, original.st_gid)
            output.write(content)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, target)
        temporary = None
    except OSError:
        parser.exit(1, "Update failed; existing .env was not replaced. Check path and permissions.\n")
    finally:
        if temporary is not None:
            os.unlink(temporary)
    print("Review login settings updated; .env permissions are 600. No values printed.")
    print("Next: docker compose run --rm --no-deps backend -check")


if __name__ == "__main__":
    main()
