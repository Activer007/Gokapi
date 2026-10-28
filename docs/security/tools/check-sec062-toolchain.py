#!/usr/bin/env python3
"""Reject an unreviewed compiler or mismatched builder before security gates."""
import argparse
import re
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[3])
parser.add_argument("--actual", help="go env GOVERSION from the compiler being used")
args = parser.parse_args()
root = args.root
reviewed = "1.26.10"  # Go security release checked on 2026-10-29; see baseline.
errors = []
for name in ("go.mod", "build/go.mod"):
    text = (root / name).read_text(encoding="utf-8")
    versions = re.findall(r"^go ([^\s]+)$", text, re.M)
    if versions != [reviewed]:
        errors.append(f"{name}: expected reviewed minimum go {reviewed}")
for name, suffix in (("Dockerfile", "alpine"),
                     ("Dockerfile.cli-tool", "alpine"),
                     ("build/Dockerfile", "bookworm")):
    text = (root / name).read_text(encoding="utf-8")
    images = re.findall(r"^FROM (golang:[^\s]+)", text, re.M)
    if images != [f"golang:{reviewed}-{suffix}"]:
        errors.append(f"{name}: builder must use reviewed go {reviewed}")
if args.actual is not None and args.actual != f"go{reviewed}":
    errors.append("actual compiler differs from reviewed compiler")
for error in errors:
    print(f"FAIL: {error}")
if errors:
    raise SystemExit(1)
print(f"PASS: module/builder policy, reviewed go{reviewed}" +
      ("; actual compiler matches" if args.actual is not None else
       "; actual compiler NOT_CHECKED"))
