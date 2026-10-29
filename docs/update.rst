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


*******************
Native deployment
*******************

Stable version
==============

Download the latest release and extract it into the same directory as your existing installation, overwriting the old binary. Then restart SupaShare.

Unstable version
=================

.. code-block:: bash

   git pull
   make

Then restart SupaShare.
