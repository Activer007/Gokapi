#!/usr/bin/env python3
"""Export login artwork; requires Pillow only when preparing new images."""
import argparse
from pathlib import Path

from PIL import Image, ImageOps

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("sources", nargs="+", type=Path)
parser.add_argument("--output-dir", required=True, type=Path)
parser.add_argument("--version", default="v1")
parser.add_argument("--quality", default=82, type=int)
args = parser.parse_args()
if not 1 <= args.quality <= 100 or not args.version.isalnum():
    parser.error("quality must be 1..100; version must be alphanumeric")
args.output_dir.mkdir(parents=True, exist_ok=True)
for source in args.sources:
    with Image.open(source) as original:
        artwork = ImageOps.fit(ImageOps.exif_transpose(original).convert("RGB"),
                               (1280, 1056), method=Image.Resampling.LANCZOS)
        target = args.output_dir / f"{source.stem}-{args.version}.webp"
        artwork.save(target, "WEBP", quality=args.quality, method=6)
        print(f"{target}: 1280x1056, {target.stat().st_size} bytes")
