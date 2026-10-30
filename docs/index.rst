.. _index:

===========================
SupaShare
===========================

SupaShare is an independently maintained, self-hosted file sharing and collection application developed from Gokapi. It targets desktop browsers and trusted small teams, with English and Simplified Chinese interfaces. Ordinary uploads require authentication; holders of valid File Request links can also upload within the request's limits.

This lets companies or individuals share files easily and have them removed automatically afterwards, saving disk space and keeping control over who can access the content.

Key features:

* **Expiring links** — files are deleted after a configurable number of downloads or after a set number of days
* **File Requests** — generate a link that lets external users upload files to your server
* **Multi-user support** — multiple accounts with granular per-user and per-API-key permissions
* **Deduplication** — avoid duplicate storage in supported encryption modes; end-to-end encrypted uploads do not use deduplication
* **End-to-end encryption** — optional client-side encryption; see the setup guide for the mode's requirements and limitations
* **S3-compatible cloud storage** — store files on AWS S3, Backblaze B2, or any S3-compatible provider instead of locally
* **CLI tool** — upload and download files directly from the command line
* **REST API** — full API for scripting and third-party integrations
* **Bilingual workspace** — English and Simplified Chinese across public pages, administration and setup
* **Configuration-based branding** — set the public name, browser title, footer text and help links; custom CSS and JavaScript remain available
* **Account handoff** — create an internal-login user and copy their generated temporary password, username and login URL; require a password change after first browser login

The documentation in this repository describes SupaShare. The historical Gokapi Read the Docs website describes the upstream project and may differ. Start with the repository's English or Chinese README for the current product overview and quick start.


Contents
========

.. toctree::
   :maxdepth: 2

   setup
   usage
   file-collections
   update
   repository-identity
   advanced
   troubleshooting
   examples
   contributions
   changelog
