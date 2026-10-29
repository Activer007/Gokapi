Repository identity
===================

This fork is maintained at https://github.com/Activer007/Gokapi.
Application/footer links, setup references, documentation, GitHub API requests
and build tooling use this repository. The Go module and internal imports use
``github.com/Activer007/Gokapi`` consistently, including the build helper module,
WebAssembly targets and linker variables used by Docker/release builds.

Inherited publication jobs remain explicitly disabled. Replacing the repository
identity must not silently enable publication to inherited image registries.
Use the local validation workflow documented in ``build/test-ui-local.sh``.

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
