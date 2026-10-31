Login artwork maintenance
=========================

The login page uses a left-hand form and a decorative illustration on the right.
The catalog contains 13 images: four original file-sharing illustrations and
nine selected mechanical, macro and abstract artworks.
Each new page load selects one entry from a local catalog. The image stays fixed
while typing. After a failed login, the browser attempts to reuse the previous
image ID stored in this tab's sessionStorage. No credentials are stored there.
Storage failures are harmless. An unavailable image falls back once to the first
catalog entry, then to the plain background if that image also fails.

At desktop widths of 960 CSS pixels or less the illustration is hidden and its
request is deferred until the window becomes wider. This does not change the
existing device-access policy. There is no slideshow, timer or third-party image
request. Configuration-based branding, title and footer remain unchanged.

Adding an image
---------------

1. Keep the original artwork and provenance under ``docs/images/``. Follow the
   chosen blue/silver metal and charcoal style; omit text and authentication UI.
2. Export a 1280 by 1056 WebP from the repository root. Pillow is required only
   for this optional authoring tool, not for building or running SupaShare::

     python build/prepare-login-art.py docs/images/my-artwork/new-art.png --output-dir internal/webserver/web/static/assets/brand/login --version v1

   The script performs centered, aspect-preserving cropping and compression at
   quality 82. Inspect the result at real display sizes. Target under 300 KiB;
   the catalog regression test rejects files above 400 KiB.
3. Add one entry inside ``html_login_art.tmpl``. Use a stable, unique lowercase
   ID and a relative resource URL::

     <span hidden data-login-art-id="new-art" data-login-art-src="./assets/brand/login/new-art-v1.webp" data-position="50% 50%"></span>

   The first entry is the fallback. ``data-position`` sets CSS object-position;
   adjust it only when the real viewport crop needs a different focus.
4. Run ``go generate ./...`` on the supported Go toolchain, then the focused
   ``ui-login-art.test.cjs`` and ``TestUILoginArtworkCatalog`` tests. Check the
   actual browser image, crop, packaged MIME and size. Commit source, generated
   assets and provenance together. The random-selection code needs no changes
   when entries are added or removed.
5. Rebuild/redeploy through the normal release process. Adding a file beside a
   running embedded binary does not change its catalog. When replacing an image,
   export a new version such as ``--version v2`` and update its catalog URL;
   keep the logical ID if it still represents the same artwork. Do not overwrite
   a versioned URL with different content and expect caches to invalidate.

Only optimized runtime images belong under ``internal/webserver/web/static``.
Do not copy full login mockups or multi-megabyte PNG originals into that directory.
The catalog is compile-time project content, not a runtime upload endpoint or
an administrative image manager. Custom CSS should scope overrides to
``.login-workspace`` or ``.login-art`` rather than every ``.public-panel``.
