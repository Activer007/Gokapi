# README image sources

These images support the English and Chinese SupaShare READMEs.

| File | Source |
| --- | --- |
| `supashare-cover.png` | Original brand artwork generated with the built-in Codex image generation tool on 2026-10-29. It is an illustration, not an application screenshot. |
| `workspace-en.jpg` | Real English workspace, SupaShare 2.11.0, source commit `8c39931733be853dd68d11ad5b193088c4d928d5`. |
| `workspace-zh.jpg` | The same workspace in Simplified Chinese. |
| `permissions-en.jpg` | Real permission editor opened for a demonstration ordinary user; no permission changes were saved. |

The screenshots were captured through the browser tool from a loopback-only, isolated SQLite/config/data fixture built from the source above. Account names and file content are synthetic. Screenshots contain no real user data, passwords or API keys. They have not been restyled or redrawn by image generation.

## Refreshing the screenshots

1. Build the target source version and generate its frontend/WASM assets.
2. Run a local instance with disposable configuration/data. Use a few harmless, readable demo filenames, a demonstration administrator and an ordinary user.
3. Capture the desktop workspace in English and Chinese after the enhanced file table and Copy link actions finish loading. Keep the application name and version visible.
4. Open the ordinary user's permission editor without saving changes. Capture it after the modal animation settles.
5. Save browser screenshots as JPEG, verify their contents, and update the source version above and the screenshot captions in both READMEs together.

The screenshots show one demonstration configuration, not a default-data installation or a hosted public demo. Preserve this distinction when updating the documentation.

## Cover prompt

The built-in image generation tool was used with the following prompt. The accepted output was copied into this repository without compositing or image edits.

```text
Use case: ads-marketing. Asset type: wide GitHub README cover for SupaShare, a self-hosted desktop file sharing and file collection application.
Create a refined, restrained editorial product cover, landscape 2.4:1 aspect ratio, 1920 x 800 if possible. Dark charcoal (#1f2329) background matching a dark desktop file manager, electric blue (#1677ff) accents, soft studio light and subtle fine surface texture.
Composition: left 48% is calm typographic negative space. Large exact white wordmark "SupaShare", on one line. Beneath it smaller exact text "Self-hosted file sharing". Right half contains one beautifully crafted sculptural blue folder with two clean pale document sheets and a small metallic link symbol, resting on a dark surface; slight three-quarter perspective, sophisticated soft shadow, quiet blue rim light. Keep the object modest, not toy-like. Clear separation, generous margins, excellent readability when displayed at 900 px wide.
The visual should communicate control over files and straightforward sharing. Do not invent an app interface or browser screen. Do not add feature cards, badges, security certifications, shield icons, version numbers, extra logos, gradients across the text, watermarks, or any other words. The illustration is brand artwork, while actual application screenshots will appear below it in the README.
```
