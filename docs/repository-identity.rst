Repository identity
===================

This fork is maintained at https://github.com/Activer007/SupaShare.
Application/footer links, setup references, documentation, GitHub API requests
and build tooling use this repository. The Go module and internal imports use
``github.com/Activer007/SupaShare`` consistently, including the build helper module,
WebAssembly targets and linker variables used by Docker/release builds.

SupaShare migration
-------------------

The software and repository are named SupaShare. The server command is now
``cmd/supashare`` and local builds produce ``supashare`` and ``supashare-cli``.
Build container images from this repository (``supashare:local`` and
``supashare-cli:local``); the upstream ``f0rc3/gokapi`` images do not contain
these changes. Update external service scripts that invoke the old binary
path to invoke the new executable before deploying the rebuilt package.

Existing configuration, database files, uploaded data and encryption keys keep
their formats and locations. ``GOKAPI_*`` environment variables, CLI credential
paths, cookies, browser storage keys, internal JavaScript/WASM bindings and
private staging markers retain their legacy identifiers for compatibility.
The systemd unit remains ``gokapi.service`` so upgrading cannot create a second
service against the same data. Existing Docker volumes and bind directories
retain their names; the Compose service is now ``supashare``.

Stop the old Gokapi container/service before starting SupaShare against the
same data. For a Compose upgrade, stop the deployment with the old Compose
file before switching to the new file, then rebuild and start ``supashare``.
Do not remove volumes or create replacement data/config directories. Changing
the Compose service name alone does not stop an existing old service.

An empty public name or the old built-in ``Gokapi`` default displays as
``SupaShare``. Custom public names, UI titles, footer text and links are
preserved. The old upstream documentation and HTTP encrypted-download helper
are separately hosted services and are not renamed or redeployed by changing
this repository. Original author/license attribution and archived raw evidence
are retained.

Version line
------------

Starting with ``2.10.0``, this fork uses its own version line to distinguish
Activer007/SupaShare builds from upstream builds. This intentional numbering jump
does not imply that this fork published versions 2.3 through 2.9.

The source of truth is ``versionGokapi`` in ``cmd/supashare/Main.go``. Change that
constant, run ``go generate ./...`` from the repository root, and commit the
generated version/API/static asset changes with the source update. Rebuild and
deploy the binary or image to update the displayed version. Git tags and
GitHub Releases are separate publication steps; merging a PR does not
automatically increment the version or publish a release.

When changing the source version, update the current-version paragraph in both
``README.md`` (Chinese) and ``README.en.md`` (English), the documentation release in ``docs/conf.py``,
and the SupaShare changelog entry. Screenshot
captions record the version actually captured; refresh the images before changing
their version labels. ``README.zh-CN.md`` remains a compatibility link to the
Chinese default README, rather than a separately maintained copy. The image guide in ``docs/images/README.md`` records their source.

Deployment and provenance
-------------------------

Inherited publication jobs remain explicitly disabled. Replacing the repository
identity must not silently enable publication to inherited image registries.
Select local checks according to the change's risk and impact, following
``CONTRIBUTING.md``. The broader ``build/test-ui-local.sh`` workflow remains
available when needed; it is not required for every small change. GitHub Actions
remain disabled under the current maintainer policy.

Raw historical security evidence in ``docs/security/evidence`` is preserved as
captured, including its original module names and repository metadata. These
records are evidence of earlier runs, not application settings or build inputs.
Historical changelog links have been redirected to this fork as requested;
issue/PR numbers in that inherited history do not imply corresponding fork
issues exist.

The legacy GitHub Pages helper used for encrypted downloads over HTTP is a
separate deployed service, not a repository URL. It is retained until a tested
self-hosted HTTPS replacement is available; changing its hostname alone would
not deploy that service.

Validation
----------

The migration passed ``build/test-ui-local.sh`` on a native Linux snapshot:
18 Node behavior tests, all Go packages with ``test,noaws`` and
``test,awsmock``, focused race tests and default/noaws server builds.
Regeneration produced no resource drift. The local browser verified the new
footer URL in both languages and reported no JavaScript errors.
