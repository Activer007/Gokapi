Desktop UI/UX improvement plan
=============================

Scope and design direction
--------------------------

Implement option B from the desktop audit: accessible actions and forms,
an efficient file workspace, and reviewable permission editing. Keep English
and Chinese, configuration-file branding, and the existing desktop-only
policy. Use the existing Go templates, Bootstrap and vanilla JavaScript.

The visual direction is a restrained dark workspace with a compact header,
clear text actions, stable table alignment and blue primary actions. Apply
Apple's principles of hierarchy, consistency and feedback to this web app;
retain native browser keyboard behavior and existing product conventions.

References:

* https://developer.apple.com/design/human-interface-guidelines/layout
* https://developer.apple.com/design/human-interface-guidelines/buttons
* https://www.w3.org/WAI/tutorials/forms/instructions/

Delivery batches
----------------

1. Accessible actions and reliable feedback

   * Keyboard-operable file selection on admin and public collection pages.
   * Persistent login labels, named create actions, unclipped bilingual forms.
   * Clipboard success/error handling, accessible notices and manual fallback.
   * Named API-key deletion confirmation with pending/error recovery.

2. Desktop file workspace

   * Compact shared navigation, top-aligned admin content and restrained styling.
   * Compact upload area and live summary of download/expiry/password settings.
   * Readable file table, primary Copy link action and secondary action menu.
   * Preserve encrypted links, hotlinks, ownership checks and dynamic rows.

3. Reviewable settings and permissions

   * Text permission summary and accessible editor for users and API keys.
   * Explicit save/cancel, dependent permissions and partial-failure recovery.
   * Consistent form state, pending/error feedback and retry without lost input.
   * Final desktop and bilingual regression review.

Acceptance
----------

Each batch is a medium-sized PR based on the current master. Fetch before
opening/merging, run local validation, and record the results in the PR.
GitHub Actions are not the acceptance gate.

Verify the real rendered flows with the in-app browser: login, keyboard file
selection, upload and copy, edit and collection creation, permission save and
cancel, deletion cancel/confirmation, failure recovery and language switching.
Use synthetic local data. Include long filenames, empty/search-empty states,
restricted users and E2E-sensitive UI where affected.

Check desktop widths 1280/1440/1920 and zoom resilience when the browser
supports fixed dimensions; record actual dimensions and any tool limitation.
Check console health and inspect saved screenshots. Run focused JavaScript
behavior tests, Go template/i18n checks, generated-resource checks and final
local validation with build/test-ui-local.sh on a native Linux filesystem.

Progress
--------

* Batch 1: implemented keyboard file selection, explicit labels/actions,
  clipboard feedback/fallback and API-key deletion confirmation. Local Node
  tests (13), Go UI/i18n tests and test-server build pass. In-app browser
  verified admin/public keyboard uploads, copy success notification and
  deletion cancellation. The browser tool's virtual clipboard cannot inspect
  ClipboardJS's native clipboard; failure/success handling is covered by Node.
* Batch 2: compact desktop navigation, restrained surfaces, upload setting
  summary and named file actions implemented. ID cells stay in the DOM for
  E2E compatibility and remain searchable; the More menu opens the current
  download link. Local Node tests (14), Go UI/i18n checks and build pass.
  Browser verified a 1280 x 720 desktop, bilingual views, setting summaries,
  ID search, no-match state and keyboard menus. Fixed viewport requests on
  existing in-app tabs were not honored; actual dimensions are recorded.
* Batch 3: reviewable user/API permissions with explicit save/cancel,
  dependency ordering and partial-failure retry; keyboard-operable API rename;
  shared pending/error handling for file, collection and user forms. Browser
  verified dependency drafts and cancellation, protected/limited permissions,
  persisted API permission revocation, Chinese rename, collection save, and a
  real duplicate-user error that keeps the draft retryable.
* Final local CI passed on a native Linux snapshot: 18 Node tests, all Go
  packages with test,noaws and test,awsmock, focused race tests and both server
  builds. E2E tests cover disabled selection on cipher failure and decrypted
  copy/QR/email links. The old API-page copy assertion was updated to verify
  the actual rename and permission controls.
* Browser dimensions verified: 1280 x 720 and an existing 965 x 1244 desktop
  panel. Requested 1440/1920 overrides were not reliably applied; those sizes
  are not claimed as verified. Synthetic data was used throughout.
* Repository follow-up: source/build paths, UI/documentation links and GitHub
  API URLs migrated to Activer007/Gokapi in a separate PR. Inherited publishing
  workflows remain disabled. See repository-identity.rst for the preserved
  historical evidence and separate legacy download-service dependency.
