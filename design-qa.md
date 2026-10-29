# SupaShare design QA — foundation batch

final result: passed

Scope: B1 workspace visual validation only. This is not acceptance of the full B0–B6 upgrade.

Source: `docs/images/ui-concepts/ui-enhanced-compact.png`, 1586×992 pixels.
Implementation: `docs/images/ui-upgrade/foundation-zh.jpg`, 1586×992 pixels, CSS viewport 1586×992, devicePixelRatio 1. Source and final implementation were displayed together in the same comparison tool result on 2026-10-29, without resizing or browser chrome. Original iteration captures remain under `local-ui.secret/ui-upgrade/foundation/`.

State: Chinese administrator, collapsed upload rules and three actual synthetic files. The download-count column and configured two-line footer remain present. Screenshot data is not a product default.

## Findings and corrections

- Fixed P2: At the reference viewport, the first implementation's heading, upload prompt and artwork were visibly undersized. A taller-desktop media query now uses a 40px heading, 30px upload prompt, 280×176 artwork slot and larger table type. The final same-size comparison confirms the stronger hierarchy and approximately 194px upload panel.
- Fixed P2: A 720px effective desktop viewport squeezed some table headings into single-character lines. Below 1100px, the table now keeps a 960px minimum width with local horizontal scrolling; expanded action menus stay in table flow. `desktop-narrow-menu-720-final.jpg` shows the focused Edit action, other menu items and scrollable layout, without page-level horizontal overflow.
- Fixed during browser verification: duplicate lower table count pushed the footer down at 1440×900; the toolbar now owns counts. The current language button now uses its actual `aria-pressed` state for styling. Empty-table text is centered.
- Fixed functional findings: dynamic additions now redraw the table; replacement labels use plain text and include filtered rows; DataTables' array-like node collection is handled without assuming an iterator.
- Fixed functional finding during final mainline verification: immediate restore could race with a deletion animation timer. The table entry is removed immediately, and the delayed callback only detaches the old DOM node. The regression test and real immediate-restore flow now retain the restored row.

## Required visual surfaces

- Typography: local system font stack retains Chinese support and avoids external font requests. The final scale correction preserves clear page, section and row hierarchy. Full-view images were readable at 1:1, so separate typography crops were unnecessary; the narrow-menu capture supplies focused interaction evidence.
- Spacing: 48px horizontal gutters, top navigation, compact disclosure and wide table follow the selected composition. Shorter desktop windows retain compact sizes and normal vertical scrolling.
- Colors: charcoal surfaces and blue primary actions/links follow the target. Calculated contrast: text/surface 13.35:1, muted/surface 7.78:1, links/surface 7.35:1, white/primary 5.59:1, input border/background 3.25:1. Keyboard focus is visible in the menu capture. These selected checks do not claim full WCAG certification.
- Imagery: independently generated transparent blue folder and metal links match the subject and material direction; no screenshot is embedded as UI. Large-source PNG size is a possible later optimization, not a new runtime dependency.
- Content: existing configurable brand, footer, version and real file metadata remain authoritative. The concept's omitted download-count column is retained intentionally. Full labels for More and language selection remain for clarity.

## Acceptance and remaining scope

- No remaining P0/P1/P2 visual findings in the B1 workspace comparison.
- Actual viewports inspected: 1280×720, 1440×900, 1920×1080; matched-reference viewport 1586×992. A 720×450 effective viewport checks layout pressure equivalent to 200% on 1440×900. Actual browser zoom percentages were not changed and are not claimed as verified.
- Keyboard disclosure opens and moves focus to the first rule switch; keyboard menus open, scroll to their last actions, and close with Escape.
- P3: the summary uses a localized sentence that explicitly describes new uploads, instead of three decorative columns. Outline file icons reuse the installed icon library. These differences preserve semantics and avoid unnecessary components.
- P3: optimize the decorative PNG further if asset transfer cost becomes material; the current local asset is approximately 1.9 MB and is cached.
- Full-site QA and encrypted content acceptance belong to later batches and are still pending.
