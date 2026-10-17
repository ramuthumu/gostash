#!/usr/bin/env python3
"""Create a private GoStash password-hash and session-key env file."""

import base64
import getpass
import hashlib
import os
import secrets
import sys
from pathlib import Path


def main() -> int:
    target = Path(sys.argv[1] if len(sys.argv) > 1 else "gostash-auth.env")
    if target.exists():
        print(f"Refusing to overwrite {target}", file=sys.stderr)
        return 1

    password = getpass.getpass("GoStash password (at least 16 characters): ")
    confirmation = getpass.getpass("Repeat password: ")
    if password != confirmation:
        print("Passwords do not match.", file=sys.stderr)
        return 1
    if len(password) < 16:
        print("Use a password with at least 16 characters.", file=sys.stderr)
        return 1

    iterations = 600_000
    salt = secrets.token_bytes(16)
    digest = hashlib.pbkdf2_hmac("sha256", password.encode(), salt, iterations)
    encode = lambda value: base64.urlsafe_b64encode(value).decode().rstrip("=")
    contents = (
        "GOSTASH_PASSWORD_HASH='pbkdf2-sha256$"
        + str(iterations) + "$" + encode(salt) + "$" + encode(digest) + "'\n"
        f"GOSTASH_SESSION_KEY='{encode(secrets.token_bytes(32))}'\n"
        "GOSTASH_COOKIE_SECURE=true\n"
    )
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as env_file:
        env_file.write(contents)
    print(f"Created {target} with mode 0600. Keep it out of Git.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
