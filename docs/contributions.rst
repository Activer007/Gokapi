.. _contributions:


=============
Contributions
=============

All contributions are very welcome! If you have an issue or would like to add a pull request, please visit our Github page:

https://github.com/Activer007/SupaShare

Local development
=================

Use the toolchain declared in ``go.mod``. From the repository root on Linux,
macOS or WSL:

.. code-block:: bash

   make build       # Generate resources and build the SupaShare server
   make build-cli   # Build the optional CLI
   make test        # Go tests using the mock S3 backend

Select focused checks by S/M/H risk as described in ``CONTRIBUTING.md``. For a
broader local validation, install Node.js and run ``bash build/test-ui-local.sh``
on a native Linux filesystem (including WSL's Linux filesystem, not ``/mnt/c``
or ``/mnt/e``). That workflow generates assets, runs Node behavior tests, Go
tests with ``test,noaws`` and ``test,awsmock``, focused UI race tests and default/
noaws builds. It does not exercise a live S3 account. The historical UI checks
are documented in ``docs/ui-development.rst``. GitHub Actions remain disabled;
do not enable or dispatch inherited publication jobs.
