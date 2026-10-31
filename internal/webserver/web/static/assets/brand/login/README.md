# Login artwork

Source PNGs and exact generation prompts: `docs/images/login-art-2026-10-31/`.
Selected UI reference: `docs/images/login-concepts-2026-10-31/login-03.png`.

Exported with `build/prepare-login-art.py`, Pillow, centered aspect-preserving fit,
1280 × 1056, WebP quality 82, method 6. Original PNGs are unchanged.

| File | Bytes |
| --- | ---: |
| linked-pair-v1.webp | 100664 |
| linked-chain-v1.webp | 101216 |
| shared-folder-v1.webp | 104714 |
| document-flow-v1.webp | 101358 |

Add/remove entries in `html_login_art.tmpl`; the first entry is the fallback.
Full extension and cache-versioning instructions: `docs/login-artwork.rst`.
