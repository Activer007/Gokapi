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

Version 2.16.0 publishes Linux amd64 and arm64 server binaries and a
``SHA256SUMS`` manifest on the `releases page
<https://github.com/Activer007/SupaShare/releases>`_. Download the matching
architecture and verify its checksum as described in :ref:`setup`.
Preserve the old binary and stopped-service snapshot before replacing the
executable. Restart with the same working directory, configuration, database,
stored files and encryption material. Docker users continue to build from this
repository; this release does not publish an image or enable inherited workflows.

2.16.0 upgrade checklist
=======================

1. Record the old source commit/binary and effective data, config and database
   paths. Several source commits report ``2.15.1``; the version string alone does
   not identify whether delivery-list migrations have already run.
2. Stop the service and all upload/download activity. Back up the complete
   configuration, database and stored content, including encryption material.
   For Redis, take a consistent backup using the Redis deployment's persistence
   tools; copying only SupaShare's filesystem directories does not back up Redis.
3. Keep that snapshot unchanged. Verify the downloaded binary checksum, or select
   the reviewed ``v2.16.0`` source tag and rebuild the local Docker image. Retain
   the old binary/image. Do not start old and new services against the same data.
4. Start 2.16.0 with the existing paths. The delivery-list migration brings SQLite
   to schema 19 and Redis to schema 11 when upgrading from earlier source
   snapshots; M8/M9 add no further schema. Existing ordinary files remain shares
   under their original rules.
5. Confirm the displayed/API version, existing file downloads, a new upload,
   collection conversion, delivery-list item downloads, collection status and
   personal logical quota. Restart once and confirm the same files and lists
   remain. Quota counts metadata records, including duplicate shares and records
   still awaiting deletion; it is not physical disk usage or guaranteed capacity.
6. If verification fails, stop 2.16.0. Restore the **complete pre-upgrade snapshot**
   into separate paths/volumes and run the matching old binary/image. Verify that
   restore before switching access back. Changes made after the snapshot will not
   be present in the rollback. Never point an older binary at the new database.

Delivery-list revocation closes the list URL only; it does not revoke ordinary
file links already obtained. New-file reminders exist only on the current
management page, with visible-page polling and a check on returning to the tab.
There is no offline notification or cross-device unread state.

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

2.16.0 candidate validation
==========================

Isolated Linux amd64 native executables running inside a cached container
runtime passed a fresh SQLite/local-storage installation, ordinary upload,
public collection submissions, a real 60-second arrival check, conversion,
individual delivery downloads and restart persistence. Source baselines
``38ebe76`` (SQLite 18) and ``e95ff55`` (SQLite 19), both reporting 2.15.1,
were upgraded separately. The schema-18 test preserved the old file, created a
new delivery list, retained quota/list state across restart, and restored the
complete old snapshot with its matching binary in a separate directory.

This does not verify a rebuilt application Docker image, real S3, Redis
deployment durability, HA or production installation. Redis quota behavior has
focused provider contract tests using an isolated in-memory Redis test server.
The Linux arm64 release target is cross-built; it has no runtime verification.
