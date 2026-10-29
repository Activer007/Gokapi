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
* Batches 2 and 3: pending.
