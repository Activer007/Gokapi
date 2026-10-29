SupaShare migration and desktop layout
=====================================

Scope
-----

1. Rename the maintained GitHub repository and software to SupaShare, keeping
   application version 2.10.0 as the independent version-line baseline.
2. Reduce the shared footer from three rows to two when deployment text and
   help links are configured: deployment text first, links and software/version
   credit second. Omit empty rows.
3. Place the management navigation in the top header row, aligned to the right,
   with the site name on the left and compact language/account controls.

Delivery
--------

* Identity PR: module/import paths, command/build outputs, visible defaults,
  repository references and migration guidance. Rename the GitHub repository
  after validating the prepared source, then update the local remote URL.
* Layout PR: shared footer and top-right navigation, with bilingual desktop
  browser verification and existing interaction checks.

Compatibility
-------------

Keep existing data/configuration formats and database schema versions. Preserve
legacy GOKAPI_* deployment variables, authentication/session and encryption
protocol identifiers, existing CLI configuration lookup and established data
volume names. Replace the old built-in site name while retaining custom names.
Historical raw evidence and upstream attribution remain identifiable. The
previously approved forceu.github.io HTTP-download helper remains deployed at
its existing URL.

Acceptance
----------

* GitHub reports Activer007/SupaShare and git fetch/push use the new remote.
* The server, CLI/build metadata, defaults, footer and current documentation
  identify the maintained product as SupaShare.
* Native Linux local validation covers Go builds/tests, generated assets and
  existing frontend behaviors. Generated resource URLs resolve.
* Browser screenshots prove a two-row configured footer and top-right first-row
  navigation in Chinese and English, with representative navigation, language
  switching and file-action interactions working.
* Preserve the desktop-only access policy. Do not redesign for mobile devices.

Verified delivery
-----------------

* The GitHub repository was renamed to Activer007/SupaShare with the original
  repository ID 1435784593 retained. Fetch and push succeeded on the new remote.
  Identity migration was delivered in PR 39.
* The independent identity commit passed the complete native Linux local CI,
  server version and CLI help checks, with no generated resource drift.
  Compatibility coverage verified empty/legacy/custom public names and retained
  custom UI settings and storage/public-URL configuration.
* The final layout snapshot passed the same complete local CI: 18 Node tests,
  Go packages under test,noaws and test,awsmock, focused race checks and both
  builds. Browser verification used a measured 1280 x 720 desktop in Chinese
  and English: navigation and brand top edges aligned at y=20; no horizontal
  overflow; configured footer contained two 24px rows. File Requests and API
  navigation and keyboard file menus worked; no console errors were reported.
* Documentation uses local image rebuild commands and the new Compose service
  name. Renaming does not publish release artifacts or deploy a running server.
