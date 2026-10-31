.. _update:

======================
Updating SupaShare
======================

.. note::
   Before updating, always check the :ref:`changelog` for breaking changes in the version you are upgrading to.
   Some releases require manual steps before or after the update.

The database schema is migrated automatically on the first start after an upgrade.
It is not possible to downgrade to an older version after a schema migration has run.

.. warning::
   **Back up your data before updating**, especially for major version upgrades.
   Copy the ``data`` and ``config`` directories (or their Docker volumes) to a safe location.

   Stop the application before taking this filesystem backup. Include the actual
   SQLite file from ``DatabaseUrl`` if it is outside those directories, any
   remaining SQLite WAL/SHM files, and all encryption material. Keep the
   pre-upgrade snapshot unchanged until the upgrade has been verified.

   Test a restore in a separate directory or separate Docker volumes. To roll
   back, restore the complete pre-upgrade snapshot and run the matching old
   binary/image. Never point the old version at the migrated database, and
   never run both versions against the same state.


***************
Docker
***************

From an updated checkout of this repository, rebuild the local image:

.. code-block:: bash

   docker build -t supashare:local .

Then stop the running container and start it again with the same command you used originally.
Named volumes (``-v gokapi-data:/app/data``) preserve all your data automatically.

If you use Docker Compose:

.. code-block:: bash

   docker compose up -d --build

The bundled Compose file builds from the local source tree. If you use a
separately published image, update and pull that explicit image reference
instead. Keep the existing config/data mounts when recreating the container.

When migrating an existing Gokapi Compose deployment, stop it with the old
Compose file before switching to this repository's ``supashare`` service.
Keep its volumes and bind directories; do not run old and new services
against the same state at the same time.


*******************
Native deployment
*******************

Published packages
==================

Use a matching SupaShare package from the `releases page
<https://github.com/Activer007/SupaShare/releases>`_ only when explicitly published.
The current 2.13.1 source version is not itself a published package. Otherwise,
build from a reviewed source checkout as below. Preserve the old binary and the
stopped-service snapshot before replacing the executable, then restart SupaShare.

Source builds
=============

.. code-block:: bash

   # In the existing source checkout, select the source revision to deploy.
   git fetch origin
   git checkout <reviewed-commit-or-tag>
   make build

Replace ``<reviewed-commit-or-tag>`` with the revision you reviewed; commit or
stash local development changes before switching. The build produces
``./supashare``. Replace the stopped deployment's binary while preserving its
configuration/data paths, then restart it. Do not run the new binary from an
empty directory and accidentally initialize a separate installation.

The 2.12.0 → 2.13.1 SQLite/local-storage upgrade, independent backup restore and
old-snapshot rollback were verified with synthetic data in the native runtime.
Compose installation and restart were verified separately using a cached runtime
layer; see the `M5 acceptance record
<https://github.com/Activer007/SupaShare/blob/master/docs/m5/M5-ACCEPTANCE.md>`_
for the exact scope and unexecuted checks.
