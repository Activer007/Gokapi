# SupaShare design QA — workspace and administration batches

final result: passed

Scope: B1–B6 visual validation. Source implementation and visual checks pass within the recorded coverage; PR delivery status is tracked separately in docs/ui-upgrade-execution.md. Actual 125%/200% browser zoom remains a user-accepted, non-blocking manual follow-up.

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


## B2 collection adaptation

Final result for B2 visual scope: passed.

The selected workspace concept supplies the visual language for collection pages; there is no supplied collection-specific mockup. The concept and each real B2 page were displayed together at 1586×992, DPR 1: `collections-zh.jpg` and `public-upload-zh.jpg` under `docs/images/ui-upgrade/`.

- Management retains top navigation, charcoal surfaces, blue primary action, left-aligned heading and broad table. All seven administrator columns remain; restricted roles keep their original column rules. Empty state has a clear next action.
- Public upload retains the blue folder/link asset, clear heading and bounded content width. Notes and capacity precede file selection; queue and submission remain distinct. This task-oriented layout intentionally omits authenticated navigation.
- Fixed functional clarity: remaining capacity no longer appears under a maximum-count label. Successful completion can be dismissed; removal controls identify their file.
- English 1280×720 management and Chinese 1920×1080 public completion remain readable. The final wide public capture uses full-page screenshot mode to avoid a browser screenshot/viewport synchronization artifact; DOM checks confirmed 1920×1080, DPR 1 and no horizontal page overflow.
- No remaining P0/P1/P2 visual issue found in these B2 surfaces. Public error/login styling and shared public-header polish continue in B4. Full B0–B6 acceptance is still pending.

## B3 administration adaptation

Final result for B3 visual scope: passed.

The selected concept and final users/API/status captures were displayed together on 2026-10-30 at CSS viewport 1586×992, DPR 1. Users and API captures are 1586×992; status uses a taller full-page capture with a scrollbar. These pages inherit the concept's design language; no separate administration mockup was supplied.

- Shared left-aligned headings, charcoal table surfaces, blue primary actions and top navigation are consistent with B1. Permission columns and masked keys remain legible.
- Fixed P2: a tall credential delivery dialog could hide its footer at 720px height. Its body now scrolls within a 600px dialog; the final English 1280×720 capture shows the Close button fully inside the viewport.
- Six status cards form one row on wide screens and two rows at 1280px. Log controls wrap without page-level horizontal overflow; longer logs use normal vertical page scrolling.
- Explicit status confirmation dialogs match other administration dialogs, retain error feedback and restore focus on close. Final cancel verification leaves logs and traffic untouched.
- No remaining P0/P1/P2 visual issue observed in these B3 surfaces. First-login, public authentication/download pages and complete acceptance continue in B4–B6.

## B4 public-page adaptation

Final result for B4 visual scope: passed.

The selected concept and download capture were displayed together at 1586×992, DPR 1; login and existing-key unlock captures were inspected at the same dimensions. These flows use centered task panels rather than reproducing an authenticated file table. The top brand/language row, charcoal surfaces, blue actions and folder artwork retain the selected visual language.

- Persistent labels, 44px fields/actions, readable helper/error text and one primary action make authentication and download steps clear. Filename and longer error text wrap inside bounded panels.
- English first-login at 1280×720 keeps all fields, submit action and footer visible. Public collection shell remains 960px wide at 1586×992 with no horizontal page overflow.
- Fixed P2/functionality: asynchronous WASM errors could leave loading controls indefinitely; download-password submission accessed a missing button; undefined transfer errors caused a second exception. Final error paths provide feedback without announcing completion, and hide the unavailable transfer action.
- Real encrypted content, bad-cipher failure and recovery were verified in Chrome. Chrome screenshot scaling is not used as matched-reference evidence; the in-app captures provide visual comparison.
- No remaining P0/P1/P2 visual issue observed in these B4 surfaces. Installation and full-site keyboard/zoom/regression acceptance remain pending.


## B5 installation adaptation

Final result for B5 visual scope: passed.

The selected concept and real Go-rendered setup page were inspected at CSS 1586×992, DPR 1. `docs/images/ui-upgrade/setup-zh.jpg` shows the final database step. The wizard retains its existing local step navigation and standalone resource boundary; the shared charcoal/blue palette, borders, form hierarchy, focus and buttons follow the selected design.

- Fixed P2: right-positioned validation popovers collapsed into a clipped vertical strip at 1280×720. Errors now participate in field layout, remain readable, and focus the failed control. The final error screenshot confirms full text and a visible primary action.
- Fixed P2: legacy overlay cards could overflow fixed wizard dimensions. Content and step navigation now scroll independently while the footer remains reachable. The long Redis form and S3 Test Configuration action were reached by keyboard.
- Labels persist for account, storage and encryption inputs. Decorative navigation glyphs are hidden from assistive technology; the page and step headings have an explicit hierarchy.
- Checked Chinese/English initial and reconfiguration views at 1280×720, 1440×900 and 1920×1080. No page-level horizontal overflow in the inspected states. No remaining P0/P1/P2 visual issue observed in this scope.
- Preview boundary: screenshots use authenticated Go-rendered HTML plus real assets served through a read-only local preview. Authenticated installation/reconfiguration writes are covered by the native integration suite, not browser submissions. Actual zoom and full-site acceptance remain pending.

## B6 final comparison and customization

Final result for B6 visual scope: passed.

On 2026-10-30, the selected 1586×992 source image and real 2.12.0 Chinese workspace (`docs/images/workspace-zh.jpg`) were displayed together at CSS 1586×992, DPR 1. The English workspace (`docs/images/workspace-en.jpg`) was captured at the same dimensions. Synthetic files and the original business columns remain intentional differences from the concept. The readable full-view comparison is sufficient for typography/spacing; the separate custom-name view supplies focused header evidence.

- Typography/spacing: the established system font, clear heading hierarchy, compact upload panel and broad table remain. No new external font or icon library was introduced.
- Palette/assets: existing charcoal/blue tokens and the independently generated folder/link asset remain unchanged. The B1 measured contrast values still apply to these unchanged tokens.
- Content: source version is 2.12.0; real rule values, file sizes/counts/dates and configured footer remain authoritative. The artwork is decorative, with no rasterized controls.
- Fixed P2: a long custom brand wrapped into several lines and displaced the workspace. One-line ellipsis now keeps the header at 70px in the actual 1280×720 check; complete text remains in its title and accessibility name. Final evidence: `local-ui.secret/ui-upgrade/delivery/custom-name-1280.jpg`, full-page 1265×832 content capture (scrollbar excluded) from a 1280×720 viewport, DPR 1. Navigation and the long footer remain reachable, with no page-level horizontal overflow.
- Verified custom CSS precedence, custom public/admin script availability after application functions, two footer links and distinct configured browser title. No captured warning/error in this final custom page.
- No remaining actionable P0/P1/P2 visual finding in the inspected scope. P3 asset-size optimization remains optional. Actual browser 125%/200% zoom could not be set reliably; the user explicitly accepted manual follow-up without blocking delivery on 2026-10-30.


## Login artwork upgrade — 2026-10-31

Final result for this login visual scope: passed.

Compared selected login-03.png and real Chinese login at 1586×992. Evidence:
docs/images/login-implementation-2026-10-31/login-zh.png and login-en.png.
The 45:55 panel, charcoal palette, blue/silver artwork and shared header/footer
follow reference 3. Individual artwork, real brand typography and version 2.16.0
are intentional differences.

- Fixed P2: shared public CSS overrode heading/control sizes; scoped specificity
  now preserves the intended hierarchy.
- Checked Chinese/English, 1280×720, 1440×900, 1920×1080 and 800px desktop width.
  No horizontal overflow observed; short views scroll naturally.
- Real successful/failed login, CSRF expiry, recovery and keyboard focus checked.
- All four images decoded; one request on normal entry, none while narrow and
  one on widening. Image failure fallback terminates without blocking login.
- Long custom brand, configured title, footer links, custom CSS/public JS checked.
- No remaining actionable P0/P1/P2 finding in this scope. Actual 125%/200% browser
  zoom remains a nonblocking manual check. No production deployment performed.
