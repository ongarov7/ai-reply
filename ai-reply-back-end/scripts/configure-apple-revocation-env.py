#!/usr/bin/env python3
"""Set Apple revocation credentials in an existing .env without printing keys."""

import argparse
import base64
import os
from pathlib import Path
import re
import subprocess
import tempfile


def identifier(value):
    if not re.fullmatch(r"[A-Z0-9]{10}", value):
        raise argparse.ArgumentTypeError("use the 10-character Apple identifier")
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", type=Path, default=Path(".env"))
    parser.add_argument("--key-file", type=Path, required=True)
    parser.add_argument("--team-id", type=identifier, required=True)
    parser.add_argument("--key-id", type=identifier, required=True)
    args = parser.parse_args()
    temporary = None
    try:
        target = args.env.resolve(strict=True)
        original = target.stat()
        lines = target.read_text(encoding="utf-8").splitlines()
        key = args.key_file.read_bytes()
        if not key.startswith(b"-----BEGIN PRIVATE KEY-----"):
            raise ValueError("use the downloaded Sign in with Apple .p8 file")
        # -noout ensures neither key material nor a rendered environment is logged.
        subprocess.run(
            ["openssl", "pkey", "-check", "-noout"], input=key,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True,
        )
        values = {
            "APPLE_TEAM_ID": args.team_id,
            "APPLE_KEY_ID": args.key_id,
            "APPLE_PRIVATE_KEY": base64.b64encode(key).decode("ascii"),
        }
        # Remove all old assignments: direct Go startup keeps the first value.
        kept = [line for line in lines if line.split("=", 1)[0].strip() not in values]
        content = "\n".join(kept).rstrip("\n") + "\n\n"
        content += "\n".join(f"{name}={value}" for name, value in values.items()) + "\n"
        fd, temporary = tempfile.mkstemp(prefix=".env.apple-", dir=target.parent)
        with os.fdopen(fd, "w", encoding="utf-8") as output:
            os.fchmod(output.fileno(), 0o600)
            current = os.fstat(output.fileno())
            if (current.st_uid, current.st_gid) != (original.st_uid, original.st_gid):
                os.fchown(output.fileno(), original.st_uid, original.st_gid)
            output.write(content)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, target)
        temporary = None
    except (OSError, ValueError, subprocess.CalledProcessError):
        parser.exit(1, "Update failed; existing .env was not replaced. Check paths, permissions, Python/OpenSSL and the private key.\n")
    finally:
        if temporary is not None:
            os.unlink(temporary)
    print("Apple revocation settings updated; .env permissions are 600. No key values printed.")
    print("Next: docker compose run --rm --no-deps backend -check")


if __name__ == "__main__":
    main()
