.. _setup:

=====
Setup
=====

.. note::
   **Most users:** pull the Docker image, run the container, open the setup URL in your browser.
   Jump straight to :ref:`quickstart_docker` if that describes you.
   The sections below cover every option in full detail.


*****************************
Installation
*****************************

There are two deployment methods (Docker and native) and two release tracks (stable and unstable).
**Stable + Docker is the recommended combination for most users.**

Docker
^^^^^^^

Pull the latest stable image:

.. code-block:: bash

   docker pull docker.io/f0rc3/gokapi:latest

For the unstable (development) build, use ``latest-dev`` instead of ``latest``.

If you prefer to build the image yourself, the Dockerfile is available on the `GitHub project page <https://github.com/Forceu/gokapi>`_.


Native Deployment
^^^^^^^^^^^^^^^^^^

Stable version
""""""""""""""

`Download the latest release <https://github.com/Forceu/gokapi/releases/latest>`_ and copy the executable into a new folder with write permissions.

Choose the binary that matches your system:

* **Windows:** ``gokapi-windows_amd64``
* **Mac (Intel):** ``gokapi-darwin_amd64``
* **Mac (Apple Silicon):** ``gokapi-darwin_arm64``
* **Linux:** ``gokapi-linux_<arch>`` matching your architecture

.. note::
   ``gokapi-XXX`` is the main server application. ``gokapi-cli-XXX`` is the optional command-line upload/download tool — you do not need it to run the server.

Unstable version
""""""""""""""""

Only recommended if you are comfortable with the command line. Requires Go 1.25 or newer.

.. code-block:: bash

   git clone https://github.com/Forceu/Gokapi.git .
   make

This compiles the source and produces a ``gokapi`` executable from the latest code.


*****************************
Running Gokapi
*****************************

.. _quickstart_docker:

Docker
^^^^^^^

.. warning::
   Always mount ``/app/data`` and ``/app/config`` as volumes. Without them, **all data is lost** when the container is removed or updated.

The standard run command:

.. code-block:: bash

   docker run -v gokapi-data:/app/data -v gokapi-config:/app/config \
     -p 127.0.0.1:53842:53842 -e TZ=UTC f0rc3/gokapi:latest

* ``-p 127.0.0.1:53842:53842`` — binds to localhost only, which is correct when using a reverse proxy.
  Replace with ``-p 53842:53842`` only if you need direct access without a proxy — note that without SSL, traffic is unencrypted.
* ``-e TZ=UTC`` — set this to your timezone, e.g. ``-e TZ=Europe/Berlin``.

Running as a non-root user:

.. code-block:: bash

   docker run --user "1000:1000" \
     -v ./gokapi-data:/app/data -v ./gokapi-config:/app/config \
     -p 127.0.0.1:53842:53842 -e TZ=UTC f0rc3/gokapi:latest

Replace ``1000:1000`` with the desired ``uid:gid``. Note that this form uses bind mounts (``./gokapi-data``) instead of named volumes — make sure the host directories exist and the user has read/write access.

See :ref:`deprecation_nonroot` if you are migrating from the old ``DOCKER_NONROOT`` environment variable.

Docker Compose
""""""""""""""

Download ``docker-compose.yaml`` and ``.env.dist`` from the repository. Rename ``.env.dist`` to ``.env`` and edit as needed.

The ``gokapi-data`` and ``gokapi-config`` folders are created automatically in the current directory. Start with:

.. code-block:: bash

   docker compose up -d

By default the container restarts automatically on boot (``restart: always``). Change to ``restart: unless-stopped`` if you only want automatic restart after a crash.

Fork non-root Compose (SEC-011)
"""""""""""""""""""""""""""""""

``docker-compose.nonroot.yaml`` is a standalone, opt-in profile for this fork. It starts the application and entrypoint as UID/GID ``10001:10001``, disables the legacy recursive permission-changing entrypoint, and publishes only on host loopback. The image default user and the original Compose file remain unchanged. The upstream ``f0rc3/gokapi`` image does not contain this fork's changes.

For a **new installation**, build your reviewed checkout and prepare two **new, empty** named volumes. These Linux shell commands refuse existing volume names. Use different names if they already exist; never substitute production volumes into the preparation helper.

.. code-block:: bash

   set -euo pipefail
   export GOKAPI_IMAGE=gokapi-local:sec011
   export GOKAPI_UID=10001 GOKAPI_GID=10001
   export GOKAPI_DATA_VOLUME=gokapi-sec011-data
   export GOKAPI_CONFIG_VOLUME=gokapi-sec011-config
   git -c core.autocrlf=false archive HEAD | docker build -t "$GOKAPI_IMAGE" -

   if docker volume inspect "$GOKAPI_DATA_VOLUME" >/dev/null 2>&1 || \
      docker volume inspect "$GOKAPI_CONFIG_VOLUME" >/dev/null 2>&1; then
       echo 'Choose fresh volume names; existing volumes require a migration plan.'
       exit 1
   fi
   docker volume create "$GOKAPI_DATA_VOLUME"
   docker volume create "$GOKAPI_CONFIG_VOLUME"
   docker run --rm --user 0:0 --entrypoint sh \
     -v "$GOKAPI_DATA_VOLUME:/fixture/data" \
     -v "$GOKAPI_CONFIG_VOLUME:/fixture/config" \
     "$GOKAPI_IMAGE" -ec '
       test -z "$(ls -A /fixture/data)"
       test -z "$(ls -A /fixture/config)"
       chown 10001:10001 /fixture/data /fixture/config
       chmod 700 /fixture/data /fixture/config
     '
   docker compose -f docker-compose.nonroot.yaml up -d --pull never

The one-time helper runs as root only to prepare the two empty volume directory entries; it does not recurse. The service itself starts as the selected non-root user. If choosing different numeric IDs, change both the helper and Compose variables consistently; neither ID may be zero. Save the image ID and chosen settings with your deployment records. For distributed releases, use a tested digest from your own image namespace; this fork has not published an image.

Keep these exports in a private deployment environment file if needed and pass it explicitly with ``--env-file``. It needs image, UID/GID and volume names, not passwords. The profile does not import the original ``.env`` into the container. Add required Redis/S3/proxy settings explicitly in your deployment configuration; never enable ``DOCKER_NONROOT=true`` with this profile. Configure trusted proxy addresses according to the reverse proxy section.

Read the bootstrap proof as described below using ``docker compose -f docker-compose.nonroot.yaml exec gokapi ...`` and the exact printed file path. Choose a SQLite path within the config volume, such as ``/app/config/gokapi.sqlite``. The image filesystem is read-only; data, logs and temporary uploads remain writable under ``/app/data``. Port ``53842`` is published on loopback by default; ``GOKAPI_PORT`` changes the host port. Use HTTPS or an SSH tunnel for remote setup.

The volumes are external: Compose refuses missing volumes and ``down`` retains them. Back up config/DB, data and encryption keys together. The existing ``docker --user`` bind-mount example remains supported, but Linux directories must already be owned and writable by that numeric user; Windows bind-mount permissions do not prove Linux volume permissions.

For **existing data**, stop writes, record the old image/UID/mounts, and take a consistent backup. Copy to independent volumes or directories, inspect the copies, and adjust ownership there only after verifying paths and contents. Do not use the fresh-volume helper on old data. Test login, upload, download and restart against the copy before switching. Roll back to the old image and original mounts only after accounting for writes made after the switch. See ``docs/security/ADR-005-NONROOT-COMPOSE.md`` for compatibility limits; old password-format rollback also requires ADR-002.

Native Deployment
"""""""""""""""""

Execute the binary from the command line or by double-clicking it.


*****************************
Initial Setup
*****************************

On the first start of this fork, Gokapi opens an authenticated setup wizard. Configuration is saved only after a successful submission. Use username ``setup`` and the password in the local ``.setup-credential-*`` file whose path is printed at startup. The password itself is never printed in service/container logs. Read that exact file as the process user (for Docker, use ``docker exec -it <container> cat <printed-path>``). Do not put this password in a URL, share it, or record it in logs.

Open ``http://127.0.0.1:53842/setup`` in your browser. Native setup listens only on IPv4 loopback. For a VPS, keep setup running and open an SSH tunnel from your workstation:

.. code-block:: bash

   ssh -N -L 53842:127.0.0.1:53842 user@vps

Then browse the same local URL. Docker still listens on its container interface so port mapping works; publish the host port with ``-p 127.0.0.1:53842:53842``. A separate proxy container can reach the container port on a private network; preserve the external Host and browser Origin, and use HTTPS. Do not expose setup over public plain HTTP.

Each setup start creates a fresh proof in a new owner-only file (0600 on Linux). Failed validation can be retried with the same proof. A successful save revokes it immediately and deletes the file; already-authenticated requests cannot save a second configuration. A restart requires the new file, including after an interrupted installation. A stale file left by a crash is not accepted. Existing config files skip initial setup entirely.

These instructions apply to builds of this fork containing SEC-010; the upstream ``f0rc3/gokapi`` images shown elsewhere on this page do not contain fork-only changes. This fork has not published a new image.

If you need to change the port before running setup, set ``GOKAPI_PORT`` — see :ref:`envvar`.

.. _setup_database:

Database
^^^^^^^^^

.. warning::
   If you choose Redis, **you must enable Redis persistence** before storing any data (e.g. add ``save 1 1`` to your ``redis.conf``). Without persistence, all data is lost on a Redis restart.

.. warning::
   The Redis password is stored in plain text in the configuration file and will be visible if you re-run setup.

By default Gokapi uses SQLite, which is fine for most deployments. Use Redis if:

* you expect high download/upload traffic, or
* your SQLite database lives on a slow disk (e.g. a network share or SD card).

Settings:

* **Type of database** — SQLite or Redis.
* **Database location** — path to the SQLite file.
* **Database host** — host and port for Redis (e.g. ``127.0.0.1:6379``).
* **Key prefix** *(optional)* — added to all Redis keys; useful when sharing a Redis instance with other applications.
* **Username / Password** *(optional)* — Redis authentication credentials.
* **Use SSL** — enables TLS for the Redis connection.

.. _setup_webserver:

Webserver
^^^^^^^^^

* **Bind to localhost** — only accept connections from the local machine. Enable this when running behind a reverse proxy.
* **Use SSL** — generates a self-signed certificate. Use this only if you are *not* behind a reverse proxy. Gokapi must be restarted to renew the certificate.
* **Save IP** — logs the downloader's IP address. This may not be GDPR-compliant depending on your jurisdiction.
* **Include filename in download URL** — appends the filename to download links, e.g. ``/d/1234/Report.pdf`` instead of ``/d?id=1234``.
* **Public Name** — shown in the page title; use your company or service name.
* **Webserver Port** — the port Gokapi listens on.
* **Public Facing URL** — the externally reachable URL used when generating download links.
* **Redirection URL** — where users are sent when they open the root URL without a valid download link.

.. note::
   In fork builds containing SEC-012, set **Public Facing URL** to the real external ``https://`` URL when TLS terminates at a reverse proxy. Session, download-password and OAuth callback cookies then use Secure even if the backend uses HTTP. Explicit local HTTP development remains supported. Forwarding headers do not override this setting. Existing deployments must revoke old sessions in a controlled maintenance/reconfiguration window and sign in again, since existing browser cookies cannot be updated retroactively. See ``docs/security/ADR-002-COOKIE-AND-PASSWORD-ROLLOUT.md`` for upgrade and rollback steps.

.. note::
   If you enable filename-in-URL together with end-to-end encryption, the filename is appended client-side after decryption and is therefore visible in the URL. This may be a privacy concern for sensitive filenames. You can disable the option, or simply edit the filename in the URL — Gokapi ignores it and always serves the correct file.

.. _setup_authentication:

Authentication
^^^^^^^^^^^^^^

Choose how users log in. Disabling authentication entirely is strongly discouraged.

Username / Password
""""""""""""""""""""

The default method. All users authenticate with a username and password. You will be asked to create the initial admin credentials in the next step.

In fork builds containing SEC-012, a successful web password change signs out all sessions belonging to that user and requires a new login.

OAuth2 / OpenID Connect
""""""""""""""""""""""""""

.. note::
   Users must have an email address associated with their OIDC account.

Use this to delegate authentication to an OIDC server such as Google, Authelia, or Keycloak.

+---------------------+---------------------------------------------------------------------------------------------------+-----------------------------------------+
| Option              | Expected Entry                                                                                    | Example                                 |
+=====================+===================================================================================================+=========================================+
| Provider URL        | The URL of the OIDC server                                                                        | https://accounts.google.com             |
+---------------------+---------------------------------------------------------------------------------------------------+-----------------------------------------+
| Client ID           | Client ID from the OIDC server                                                                    | [random string]                         |
+---------------------+---------------------------------------------------------------------------------------------------+-----------------------------------------+
| Client Secret       | Client secret from the OIDC server                                                                | [random string]                         |
+---------------------+---------------------------------------------------------------------------------------------------+-----------------------------------------+
| Admin email         | Email address that identifies the super-admin account                                             | gokapi@company.com                      |
+---------------------+---------------------------------------------------------------------------------------------------+-----------------------------------------+
| Recheck identity    | How often to re-verify the user's identity with the OIDC server.                                  | 12 hours                                |
|                     |                                                                                                   |                                         |
|                     | If the server remembers consent, users will not see a login prompt again.                         |                                         |
|                     |                                                                                                   |                                         |
|                     | If it does not, each recheck triggers an active login prompt — use a longer interval.             |                                         |
+---------------------+---------------------------------------------------------------------------------------------------+-----------------------------------------+
| Restrict to groups  | Only allow members of specific groups                                                             | true                                    |
+---------------------+---------------------------------------------------------------------------------------------------+-----------------------------------------+
| Scope for groups    | The OIDC scope that carries group membership                                                      | groups                                  |
+---------------------+---------------------------------------------------------------------------------------------------+-----------------------------------------+
| Only existing users | Do not create a new Gokapi account automatically on first OIDC login                              | checked                                 |
+---------------------+---------------------------------------------------------------------------------------------------+-----------------------------------------+
| Authorised groups   | Semicolon-separated list of groups. ``*`` is a wildcard.                                          | admin;dev;gokapi-\*                     |
+---------------------+---------------------------------------------------------------------------------------------------+-----------------------------------------+

.. note::
   If a user is disabled in the OIDC provider, they can still log in to Gokapi until the *Recheck identity* interval expires.
   To revoke access immediately, delete the user's Gokapi account from the Users page.

.. note::
   If the OIDC provider remembers consent, logging out through the Gokapi interface may not fully log the user out.

When registering Gokapi as a client in your OIDC server, set the **redirect URL** to:
``http[s]://[your-gokapi-url]/oauth-callback``

Step-by-step configuration guides for specific providers are in the :ref:`examples` section:

* :ref:`oidcconfig_authelia`
* :ref:`oidcconfig_keycloak`
* :ref:`oidcconfig_google`
* :ref:`oidcconfig_entra`

Header Authentication
""""""""""""""""""""""

Use this only if your reverse proxy handles authentication (e.g. Authelia or Authentik) and forwards the authenticated username as a request header. Keycloak does not support this mode.

Enter the header key that contains the username (e.g. ``Remote-User`` for Authelia, ``X-authentik-username`` for Authentik) and the username of the admin account.

Fork builds containing SEC-013 accept this header only from an immediate connection listed in ``GOKAPI_TRUSTED_PROXIES``. Configure the proxy's actual outgoing IP or a dedicated trusted CIDR, including ``::1`` if using IPv6 loopback. The proxy must authenticate users and overwrite incoming identity headers. Restrict the backend port to that proxy; forwarding headers cannot make a direct client trusted. Duplicate username headers are rejected before account lookup or creation.

If *Only allow already existing users to log in* is enabled, new usernames coming from the proxy will be rejected until an account is created through the UI.

Disabled / Access Restriction
"""""""""""""""""""""""""""""""

.. warning::
   This option is **very dangerous**. Only use it if you fully understand the implications and your reverse proxy is correctly configured.

This disables all of Gokapi's internal authentication except for API calls. The following paths **must** be protected by your reverse proxy:

- ``/admin``
- ``/apiKeys``
- ``/auth/token``
- ``/changePassword``
- ``/downloadPresigned``
- ``/e2eSetup``
- ``/filerequests``
- ``/logs``
- ``/uploadChunk``
- ``/uploadStatus``
- ``/users``

.. _setup_storage:

Storage
^^^^^^^^^^

Choose where uploaded files are stored.

Local Storage
"""""""""""""""""

Files are stored in the ``data`` subdirectory by default.

.. _cloudstorage:

Cloud Storage (S3-compatible)
""""""""""""""""""""""""""""""""""

.. note::
   Without encryption enabled later in setup, files are stored in plain text on the cloud provider.

Stores files on an S3-compatible service such as Amazon S3 or Backblaze B2.

Create a dedicated private bucket for Gokapi. For each download, Gokapi generates a short-lived pre-signed URL so the user downloads directly from the storage provider without routing traffic through your server. If the storage is not publicly reachable, or if you prefer not to expose storage URLs, enable the proxy download option — Gokapi will retrieve the file and stream it to the user itself.

Required fields:

+-----------+-----------------------------------------------+-----------------------+-----------------------------------+
| Field     | Description                                   | Required              | Example                           |
+===========+===============================================+=======================+===================================+
| Bucket    | Name of the bucket                            | Yes                   | gokapi                            |
+-----------+-----------------------------------------------+-----------------------+-----------------------------------+
| Region    | Region name                                   | Yes                   | eu-central-1                      |
+-----------+-----------------------------------------------+-----------------------+-----------------------------------+
| KeyId     | API key ID                                    | Yes                   | keyname123456789                  |
+-----------+-----------------------------------------------+-----------------------+-----------------------------------+
| KeySecret | API key secret                                | Yes                   | verysecret123                     |
+-----------+-----------------------------------------------+-----------------------+-----------------------------------+
| Endpoint  | Custom endpoint. Leave blank for AWS S3.      | Only for Backblaze B2 | s3.eu-central-001.backblazeb2.com |
+-----------+-----------------------------------------------+-----------------------+-----------------------------------+

If you plan to use end-to-end encryption with cloud storage, configure your bucket's CORS rules to allow requests from your Gokapi URL.

.. _setup_encryption:

Encryption
^^^^^^^^^^^

.. warning::
   The encryption implementation has not been independently audited. Evaluate this risk before using it for sensitive data.

Three levels are available:

+------------------------------+---------------+---------------------------------+---------------------------------+-------------------------+
|                              | No Encryption | Level 1 — Local only            | Level 2 — Full                  | Level 3 — End-to-End    |
+==============================+===============+=================================+=================================+=========================+
| File Encryption              | None          | Local files only                | Local and cloud storage         | Local and cloud storage |
+------------------------------+---------------+---------------------------------+---------------------------------+-------------------------+
| Hotlink Support              | Yes           | Yes                             | Local files only                | No                      |
+------------------------------+---------------+---------------------------------+---------------------------------+-------------------------+
| Download Progress Indication | Yes           | Cloud storage only              | No                              | No                      |
+------------------------------+---------------+---------------------------------+---------------------------------+-------------------------+
| Download Speed               | Full          | May be slower for local files   | Slower for remote files         | Slower for all files    |
+------------------------------+---------------+---------------------------------+---------------------------------+-------------------------+

* **Level 1** encrypts files stored locally; cloud files are unencrypted.
* **Level 2** encrypts both local and cloud files. Cloud file decryption is done client-side; a 2 MB library is downloaded on first visit.
* **Level 3 (end-to-end)** encrypts files in the browser before upload. Even a fully compromised server cannot read the file contents. Downloads on mobile devices may be noticeably slower.

The encryption key can be stored in the configuration file (convenient, lower security) or entered as a master password at each startup (more secure, requires manual input).

.. note::
   If you re-run setup and enable encryption, existing unencrypted files remain unencrypted. Changing any encryption setting deletes all already-encrypted files.

.. warning::
   Firefox is currently not completly compatible with end-to-end encryption, which may result in truncated files when downloading end-to-end encrypted files with a Firefox browser


*****************************
Changing Configuration
*****************************

To change settings after the initial setup (e.g. password, storage backend, authentication method), run Gokapi with ``--reconfigure``. Use username ``setup`` and the fresh local credential file printed by this run, following the same local browser/SSH tunnel procedure as initial setup. The old proof and previously cached browser password no longer work. No password is printed in the console.

Native:

.. code-block:: bash

   ./gokapi --reconfigure

Docker:

.. code-block:: bash

   docker run --rm -p 127.0.0.1:53842:53842 \
     -v gokapi-data:/app/data -v gokapi-config:/app/config \
     f0rc3/gokapi:latest /app/run.sh --reconfigure

.. note::
   Stop the ``--reconfigure`` container and restart your normal container after completing the setup. All users will be logged out.


*****************************
Reverse Proxy
*****************************

Running Gokapi behind a reverse proxy is strongly recommended for production deployments. The proxy handles SSL termination and exposes Gokapi on standard ports.

Required proxy settings:

* **Timeout** — at least 300 seconds (to accommodate large file uploads).
* **Maximum request body size** — set high enough for your largest expected upload.
* **Forwarded headers** — pass ``X-Real-IP`` and ``X-Forwarded-For`` so Gokapi sees the real client IP.

Fork builds containing SEC-014 allow five seconds to receive complete HTTP request headers and close idle keep-alive connections after one minute. These limits apply to setup as well. They do not replace the main server's two-hour request-body and twelve-hour response limits, or the existing SSE lifetime. Proxies should forward headers promptly and retain appropriate file-transfer timeouts. Disconnected requests leave authentication rate-limit queues; already-running storage operations are not automatically rolled back.

If your proxy's outgoing IP is not ``127.0.0.1``, add it to ``GOKAPI_TRUSTED_PROXIES`` — see :ref:`availenvvar`.

If Gokapi is behind Cloudflare, set ``GOKAPI_USE_CLOUDFLARE=true``. In fork builds containing SEC-013, ``CF-Connecting-IP`` is accepted only from a trusted immediate peer and must contain one valid IP. For direct Cloudflare connections, explicitly maintain its official IP ranges in the trust list and restrict origin access. If a local proxy forwards Cloudflare traffic, trust that proxy and ensure it removes CF headers from non-Cloudflare requests.

Fork builds containing SEC-013 disable automatic trust of the whole Docker subnet by default. Configure the proxy IP explicitly before upgrading. ``GOKAPI_DISABLE_DOCKER_TRUSTED_PROXY=false`` retains the old subnet-wide trust as an explicit compatibility option and trusts every member for identity headers too. Prefer a fixed proxy IP or a dedicated network containing only trusted peers. See ``docs/security/ADR-003-TRUSTED-PROXY-BOUNDARY.md`` for migration and rollback.

X-Forwarded-For is processed from right to left through trusted proxies. A malformed hop falls back to the connection address; X-Real-IP is used only when X-Forwarded-For is absent. Proxies must overwrite or correctly append these headers.

.. note::
   Cloudflare's free plan limits upload chunks to 100 MB. If you use Cloudflare, keep ``GOKAPI_CHUNK_SIZE_MB`` at or below 100.

A complete Nginx configuration example is in the :ref:`examples` section: :ref:`nginx_config`.

For other reverse proxies (Caddy, Traefik, Apache), the same principles apply: increase timeouts, increase body size limits, and forward the real client IP. Consult your proxy's documentation for the specific directives.


*********************************
Installing as a systemd Service
*********************************

.. warning::
   Complete the initial setup by running Gokapi manually at least once before installing it as a service.

.. note::
   Only supported on Linux systems using systemd. An error is shown on unsupported systems.

.. code-block:: bash

   sudo ./gokapi --install-service

To uninstall:

.. code-block:: bash

   sudo ./gokapi --uninstall-service

Gokapi detects the user who invoked ``sudo`` and runs the service as that user. Running as root is not permitted.


.. _deprecation_nonroot:

********************************************************
Migration from DOCKER_NONROOT to docker --user
********************************************************

The ``DOCKER_NONROOT`` environment variable is deprecated. To migrate to ``docker --user``:

.. code-block:: bash

   # Copy data out of the container (directories must not exist yet)
   docker cp gokapi:/app/config ./gokapi-config
   docker cp gokapi:/app/data ./gokapi-data

   # Remove the old container
   docker rm -f gokapi

   # Start a new container as the current user
   docker run --user "$(id -u):$(id -g)" \
     -v ./gokapi-data:/app/data -v ./gokapi-config:/app/config \
     -p 127.0.0.1:53842:53842 -e TZ=UTC f0rc3/gokapi:latest

Replace ``gokapi`` with your actual container name if different. Replace ``$(id -u):$(id -g)`` with explicit IDs if you want a specific user. Ensure the bind-mount directories are owned by that user.
