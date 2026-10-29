# SupaShare

**English** · [简体中文](README.zh-CN.md)

![SupaShare — self-hosted file sharing, with a blue folder and a sharing link on a charcoal background](docs/images/supashare-cover.png)

**Share files. Collect deliveries. Keep control.**

SupaShare is a self-hosted file sharing and collection application for teams using desktop browsers. Share files with download limits and expiry dates, collect files through request links, and manage access from a focused dark workspace. English and Simplified Chinese are built in.

Developed from Gokapi and independently maintained by Activer007, SupaShare has its own interface, improvements and version line. The current source version is **2.11.0**. Build from this repository; use the [Releases page](https://github.com/Activer007/SupaShare/releases) to check for explicitly published SupaShare packages. Upstream Gokapi images do not include these changes.

[Quick start](#quick-start) · [Interface](#interface) · [Site customization](#site-customization) · [Documentation](#documentation)

## Made for everyday team sharing

| Capability | What you can do |
| --- | --- |
| Controlled sharing | Set a download limit, expiry date and optional password for a file. |
| File collection | Give external contributors a File Request link to upload within its limits, without creating an account. |
| Bilingual workspace | Switch between English and Simplified Chinese across setup, public pages and administration. |
| Your site identity | Set the public name, browser title, footer text and help links in the existing configuration file. |
| Easier account handoff | Create an internal-login user, then copy their username, generated temporary password and login URL together. The user must change the password after their first browser login. |
| Clear administration | Review user/API-key permissions, copy file links, and use explicit save/cancel actions with recoverable error feedback. |
| Storage choices | Store files locally or on S3-compatible storage; choose the encryption mode that fits your deployment. |
| Automation and identity | Use the REST API and CLI, or integrate an OpenID Connect provider. |

Ordinary uploads require authentication. File Request links grant their holders a limited upload capability. Deduplication is available where the storage/encryption mode supports it; end-to-end encrypted uploads do not use deduplication. See the [setup guide](docs/setup.rst) for encryption tradeoffs.

Independent development also includes fixes to setup credentials, session revocation, proxy trust checks and upload authorization/resource handling, plus bounded recovery of interrupted uploads. The [changelog](docs/changelog.rst) summarizes delivered changes; the [security task records](docs/security/SECURITY-TASK-BOARD.md) distinguish verified scope from deferred work.

## Interface

The desktop workspace keeps upload settings and sharing actions close to the file list. These are real **2.11.0** application screenshots with synthetic demonstration data. The cover above is brand artwork.

**English workspace**

![English SupaShare workspace showing upload limits, expiry settings, three example files and Copy link actions](docs/images/workspace-en.jpg)

<details>
<summary>View the Chinese workspace and permission editor</summary>

**中文工作台**

![中文版 SupaShare 工作台，包含上传设置、文件列表和复制链接操作](docs/images/workspace-zh.jpg)

**Reviewable permissions**

![SupaShare user permission editor with named permissions and explicit Save and Cancel actions](docs/images/permissions-en.jpg)

</details>

## Quick start

For a **new installation**, install Git and Docker with Compose, then run these commands in a POSIX shell (Linux, macOS or WSL):

```bash
git clone https://github.com/Activer007/SupaShare.git
cd SupaShare
cp .env.dist .env
docker compose up -d --build
docker compose logs --tail=80 supashare
```

The included Compose file builds `supashare:local`, stores data in `./gokapi-data` and configuration in `./gokapi-config`, and publishes port `53842` on the host's loopback interface.

1. Find the **setup credential file path** in the startup output. The password itself is not printed in logs.
2. Replace `<printed-path>` below with that exact path and read the file:

   ```bash
   docker compose exec supashare cat '<printed-path>'
   ```

3. Open [http://127.0.0.1:53842/setup](http://127.0.0.1:53842/setup) in a desktop browser. Sign in with username `setup` and the password from the file, then complete the wizard. This setup credential is separate from the administrator account you create.

For a remote server, open an SSH tunnel from your computer before visiting the same local URL:

```bash
ssh -N -L 53842:127.0.0.1:53842 user@your-server
```

Use HTTPS through a reverse proxy for production access, and configure the public server URL to match the address users will visit. For trusted proxy settings, native installation and the opt-in non-root Compose deployment, follow the [installation guide](docs/setup.rst). The default Compose deployment does not enable the separate non-root profile.

**Existing installation?** Follow the [migration notes](docs/repository-identity.rst) first. Stop the old Gokapi service/container before starting SupaShare against the same data; retain the existing volumes, configuration and encryption keys.

## Site customization

Merge these fields into your existing `config.json` (with the default Compose setup, `./gokapi-config/config.json`). Preserve the other settings, then restart SupaShare:

```json
{
  "PublicName": "Team files",
  "UI": {
    "DefaultLanguage": "en",
    "Title": "Team file centre",
    "FooterText": "For our team and collaborators",
    "FooterLinks": [
      { "Text": "Help", "URL": "https://example.com/help" }
    ]
  }
}
```

`DefaultLanguage` accepts `en` or `zh-CN`; visitors can switch languages. Custom names and footer text appear as written, so provide bilingual text when needed. The shared footer also shows the software version. These settings survive reconfiguration. See [advanced configuration](docs/advanced.rst) for custom CSS, JavaScript and favicon support.

## Deployment and compatibility

- **Desktop browsers:** common phone/tablet browser identifiers receive an HTTP 403 desktop-access message. This is a User-Agent policy, not device authentication. Desktop browsers and API/CLI clients remain supported.
- **Deployment scope:** current hardening and recovery work targets a single-process deployment for a trusted small team. Multi-instance/high-availability behavior is outside the validated scope.
- **Compatible identifiers:** `GOKAPI_*` environment variables and established data/configuration paths retain their names for existing installations. They are intentional, not stale branding.
- **Resource planning:** allow disk space for stored files and temporary uploads. Memory, CPU and storage needs depend on file sizes, concurrency and encryption mode; the project does not claim a universal minimum or throughput benchmark.
- **Recovery:** uncertain upload states may require operator review. Consult the [upload recovery runbook](docs/security/UPLOAD-RECOVERY-RUNBOOK.md) rather than deleting files or resetting storage blindly.

## Documentation

| Guide | Contents |
| --- | --- |
| [Installation](docs/setup.rst) | Docker, native builds, setup credentials, HTTPS/proxies and non-root deployment |
| [Usage](docs/usage.rst) | Sharing, File Requests, user onboarding, permissions and API keys |
| [Configuration](docs/advanced.rst) | Languages, branding, storage, environment variables and upload budgets |
| [Upgrade and migration](docs/repository-identity.rst) | SupaShare identity, retained identifiers and version management |
| [Changelog](docs/changelog.rst) | SupaShare changes and clearly separated inherited history |
| [Troubleshooting](docs/troubleshooting.rst) | Operational diagnostics |
| [Security policy](SECURITY.md) | How to report a vulnerability privately |

Documentation in this repository is the reference for SupaShare. Upstream Gokapi documentation describes a different maintained project and may not match this version. The legacy encrypted-download helper used over HTTP remains an external dependency; see the migration notes.

## Development

Use the Go toolchain declared in [go.mod](go.mod). From the repository root, on Linux/macOS or WSL:

```bash
make build       # Generate assets and build the SupaShare server
make build-cli   # Build supashare-cli
make test        # Run Go tests with the mock S3 backend
```

For the broader local validation workflow, install Node.js as well and run from a **native Linux filesystem** (including WSL's Linux filesystem, not `/mnt/c` or `/mnt/e`):

```bash
bash build/test-ui-local.sh
```

This runs asset generation, frontend behavior tests, both `test,noaws` and `test,awsmock` Go suites, focused UI race tests, and default/noaws server builds. It does not exercise a live S3 account. See [contributing](CONTRIBUTING.md) and the [local validation notes](docs/ui-development.rst).

Report reproducible bugs and suggest features through [Issues](https://github.com/Activer007/SupaShare/issues). Use the private route in the security policy for vulnerabilities.

## Origin and license

SupaShare builds on **Gokapi**, originally created by **Marc Ole Bulling**, and the work of its contributors. We retain that provenance and the existing [GNU AGPL v3 license](LICENSE.md). SupaShare's independent development starts at version `2.10.0`; this does not imply releases `2.3` through `2.9` were published by this project.

Thank you to the original authors and all [contributors](https://github.com/Activer007/SupaShare/graphs/contributors) who improve the project.
