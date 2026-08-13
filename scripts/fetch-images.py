#!/usr/bin/env python3
"""Download problem-description images for offline rendering in the TUI.

Scans description_html for <img src> URLs and saves them, in order of
appearance, to ~/.local/share/dojo/img/<slug>/<NN>.<ext>. The TUI maps the
N-th *[image: …]* placeholder in a description to the N-th file here.

Dependency-free, resumable (existing files are skipped), and polite:
~2 requests/second against the CDN.

    python3 scripts/fetch-images.py [--db PATH] [--only SLUG]
"""

from __future__ import annotations

import argparse
import random
import re
import sqlite3
import time
import urllib.request
from pathlib import Path

HEADERS = {"User-Agent": "dojo-ingest/0.1 (personal offline practice tool)"}
IMG_RE = re.compile(r'<img[^>]*?src="(https?://[^"]+)"', re.IGNORECASE)
EXT_RE = re.compile(r"\.(png|jpe?g|gif|svg)$", re.IGNORECASE)


def fetch(url: str, dest: Path) -> bool:
    req = urllib.request.Request(url, headers=HEADERS)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            data = resp.read()
    except Exception as e:  # noqa: BLE001 — log and move on, one bad URL is fine
        print(f"  !! {url}: {e}")
        return False
    dest.write_bytes(data)
    return True


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", default=str(Path.home() / ".local/share/dojo/dojo.db"))
    ap.add_argument("--only", help="fetch a single problem slug")
    args = ap.parse_args()

    root = Path.home() / ".local/share/dojo/img"
    db = sqlite3.connect(args.db, timeout=30)
    q = "SELECT slug, description_html FROM problems WHERE description_html LIKE '%<img%'"
    params: tuple = ()
    if args.only:
        q += " AND slug = ?"
        params = (args.only,)

    rows = db.execute(q, params).fetchall()
    total = done = skipped = failed = 0
    for slug, html in rows:
        urls = IMG_RE.findall(html)
        for i, url in enumerate(urls, start=1):
            total += 1
            m = EXT_RE.search(url.split("?")[0])
            ext = (m.group(1).lower() if m else "png").replace("jpeg", "jpg")
            dest = root / slug / f"{i:02d}.{ext}"
            if dest.exists() and dest.stat().st_size > 0:
                skipped += 1
                continue
            dest.parent.mkdir(parents=True, exist_ok=True)
            if fetch(url, dest):
                done += 1
                print(f"  {slug}/{dest.name}")
            else:
                failed += 1
            time.sleep(0.4 + random.random() * 0.3)

    print(f"images: {done} downloaded, {skipped} already present, "
          f"{failed} failed, {total} total across {len(rows)} problems")


if __name__ == "__main__":
    main()
