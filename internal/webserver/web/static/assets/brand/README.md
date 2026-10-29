# SupaShare UI brand asset

## share-folder-v1.png

- Created: 2026-10-29.
- Tool: built-in image generation tool, generate mode with two local style references, `transparent_background: true`.
- Purpose: small decorative folder/link illustration in the desktop upload panel; reusable in related public screens without embedding interface text.
- References: `docs/images/ui-concepts/ui-enhanced-compact.png` (selected UI direction) and `docs/images/supashare-cover.png` (materials and brand style).
- Delivered dimensions: 1536 × 1024 (requested approximately 768 × 512). PNG RGBA, 1,915,798 bytes, original generated output copied without image edits.
- Suggested rendered slot: approximately 240 × 150 CSS pixels with `object-fit: contain`; preserve aspect ratio.
- Visual check: one electric-blue folder, two pale folded-corner sheets, two interlocking metal links; no text, UI, extra symbols or rectangular backdrop. Main subject is fully visible.
- Alpha check: genuine alpha; 869,617 of 1,572,864 pixels are fully transparent. Alpha range 0–254; subject centers are near opaque (253), background samples are 0. A faint partially transparent contact shadow remains below the folder. Transparent pixels may retain background RGB values; rendering must respect alpha.
- Existing reference assets were not modified. Runtime markup should treat this as decorative (`alt=""`) when accompanying a text upload instruction.

## Final generation prompt

```text
Use case: stylized-concept. Generate one production-ready transparent PNG illustration for SupaShare's desktop file upload panel.
Input image 1 is the selected UI concept: match the small blue folder and linked-chain illustration in its upload area. Input image 2 is the brand cover and a close reference for exact materials, color, lighting and shape. These are style references, not edit targets.
Subject: one electric-blue sculptural file folder, with exactly two pale silver-white document sheets protruding upward, their upper-right corners folded, and one pair of interlocking brushed-metal chain links leaning against the folder's lower-right corner. Match the reference three-quarter camera angle, front folder flap, subtle blue rim light, softly rounded bevels, restrained fine surface texture and high-quality realistic studio render. Elegant rather than toy-like.
Composition: single centered compact subject on a genuinely TRANSPARENT background. Aim for a 768 x 512 canvas, landscape 3:2. Subject fills about 80% width and 88% height, with modest clear transparent padding so no paper, folder or chain touches an edge. It will display at approximately 240 x 150 pixels inside a charcoal interface. Preserve a crisp recognizable silhouette and readable separation at small size.
No text, no wordmark, no labels, no interface, no arrows, no additional icons, no decorative particles. No floor, pedestal, vignette or rectangular backdrop. Background must have actual alpha transparency, not white, black or baked checkerboard. Only a very subtle soft contact shadow immediately beneath the object may remain with partial alpha. No large glow halo.
```

