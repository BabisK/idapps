---
id: SPEC-native-launcher
companions:
  - catalogue-format.md
  - runtime-and-updates.md
  - distribution-and-storage.md
  - app-compatibility.md
sources:
  - ../spec-powershell-commands/SPEC.md
---

# Native launcher and application catalogue

## Why

Developers need the same Docker-backed commands on Windows, Linux and macOS, with native process behavior and app updates that remain simple text changes. A shared Go launcher using the Docker Engine API removes the script-to-Docker-CLI boundary. A separately downloaded JSON catalogue lets maintainers change image versions and add or remove apps without rebuilding the launcher.

## Capabilities

- **CAP-1**
  - **intent:** Install the launcher and invoke apps by their existing names without displacing installed commands.
  - **success:** A user downloads the appropriate native release without a repository checkout or Go toolchain. The first successful `idapps update` creates links for every catalogue app. Bare names, full link paths and `idapps NAME [ARG…]` dispatch every supported command and service on Windows, Linux and macOS. Existing aliases, functions and native executables retain precedence after documented setup; `idapps NAME` selects IDApps directly when NAME is shadowed.
- **CAP-2**
  - **intent:** Maintain app definitions as text independently of launcher releases.
  - **success:** The format in `catalogue-format.md` represents all 22 existing apps in one `apps` map, with a complete image definition inside each app and no separate image map. Changing an image or adding, changing or removing an app within that format requires only a catalogue edit and `idapps update`. Invalid definitions and incompatible launcher requirements are rejected before activation.
- **CAP-3**
  - **intent:** Refresh available apps and their definitions with one command.
  - **success:** `idapps update` fetches `catalogue.json` from the current `main` commit of `BabisK/idapps`, creates new links, repairs missing owned links and removes obsolete owned links. It is repeatable, needs no running Docker engine, rolls back ordinary failures, and recovers interrupted updates before dispatch. Unrecoverable filesystem interference is reported without running mixed state. Cached apps remain runnable without access to GitHub.
- **CAP-4**
  - **intent:** Run containerized tools with native arguments, terminal interaction and streams.
  - **success:** Direct local Docker API execution forwards arguments received by the process without shell reconstruction; preserves non-TTY bytes, separate output streams and container exit status; and supports interactive input, resize and cancellation. Redirection disables TTY allocation, including PowerShell `> file`. Missing or unusable engines fail with actionable errors. Caller-shell limitations are explicit in `runtime-and-updates.md`.
- **CAP-5**
  - **intent:** Manage existing development services through their familiar command names and lifecycle.
  - **success:** All services retain default/start, stop, logs and applicable clean operations, ports and data persistence. `logs -f` follows and `logs -t N` selects the tail. Stop preserves volumes; clean refuses running, in-use or unowned resources. Catalogue updates neither restart services nor modify their data.
- **CAP-6**
  - **intent:** Receive usable native releases and reliable catalogue updates.
  - **success:** Release artifacts, checksums and setup instructions cover the declared Windows/Linux/macOS support matrix, including standalone Windows executables and plain binaries on Linux/macOS. WinGet distribution and Apple Developer signing and notarization are deferred. CI validates the catalogue and its compatibility with supported launchers; Renovate tracks catalogue image versions. The repository and release artifacts use native entry points without the former app wrappers. Native launcher tests run on the runners specified in `app-compatibility.md` without depending on external network services. Real-engine and image-compatibility checks are performed separately as manual verification when suitable machines and images are available; results and outstanding gaps are recorded without claiming untested coverage.

## Constraints

- Use Go and one shared launcher implementation, compiled separately for each supported OS/architecture. All app links target that installation's single executable; do not build a binary per app.
- Use the Docker Engine API directly. A compatible local Linux-container Engine or Desktop instance is required; `docker`/`docker.exe`, a Go installation and wrapper-shell runtimes are not required by the launcher. This replaces the former POSIX-shell runtime dependency for the migrated application.
- Support Linux, Apple-silicon macOS and Microsoft-supported Windows operating systems where compatible local Docker prerequisites exist, using the approved release and verification matrix in `app-compatibility.md`. Invocation from PowerShell 5 and newer remains in scope; record exact tested platforms rather than claiming tests cover every supported system. Linux binaries must not depend on a particular libc implementation.
- Support local Docker endpoints only. Reject selected remote contexts rather than silently targeting another engine.
- Reuse locally cached image references and pull missing images anonymously on app launch. `idapps update` changes only the catalogue and owned links; it does not pull images. Changed tags/digests apply to subsequent launches, while upstream changes under an unchanged tag do not trigger an automatic refresh. Authentication failures must be explicit.
- Create actual symbolic links: `APP` on Linux/macOS and `APP.exe` on Windows. Failure to obtain symlink rights is an actionable error; do not substitute copies, hard links or scripts or alter OS security settings.
- Use the release formats, OS cache locations and durable records in the OS configuration directory in `distribution-and-storage.md`. Downloaded catalogues remain disposable; ownership and recovery metadata must survive cache clearing and binary replacement.
- Append the per-user app-bin directory to PATH in documented setup. Do not install shell aliases/functions or reorder existing PATH entries; `idapps NAME` or explicit full-path invocation selects IDApps during conflicts.
- Support direct `idapps NAME [ARG…]` invocation for every catalogue app, retaining `idapps run NAME [ARG…]` as an explicit alias. Reserve launcher management names in catalogue keys and pass all arguments after NAME to that app without external command lookup.
- The catalogue is declarative, versioned and validated before use. It cannot execute host scripts or arbitrary installation hooks. Trust the repository's `main` publication over HTTPS; catalogue acceptance is not permission to change files outside managed cache, state and link paths.
- Use exact resource identity and ownership for lifecycle operations. Update never deletes images, containers or volumes and never touches unrelated files or links.
- Preserve all 22 apps and the compatibility details in the companions. Retain the project's local-development-only purpose and documented default credentials.
- Remove the existing POSIX/PowerShell app wrappers, their shared runtime helpers and wrapper-specific tests when the native implementation lands. Replace setup documentation, CI and dependency discovery as specified in `app-compatibility.md`; do not retain compatibility script entry points.

## Non-goals

- Remote Docker daemons, Windows containers, Intel macOS releases or production deployment.
- Automatic launcher self-update, Docker installation, OS privilege changes or shell-profile management.
- WinGet distribution, including manifest creation, validation, installation testing and publication, is deferred.
- Private-registry login/credential-helper integration, arbitrary host hooks or a generic replacement for every Docker CLI option.
- Automatic adoption of legacy resources or database-data migration. Removing repository wrappers does not authorize deleting existing user installations or Docker resources.
- Reversing argument or byte transformations already performed by the calling shell; guaranteeing byte-safe PowerShell 5 pipelines is outside the launcher's control.

## Success signal

On Windows, Linux and macOS, install one release, run `idapps update`, then invoke `psql` by name and connect it to the catalogue's `postgres` service. With another `psql` command taking precedence, `idapps psql` still launches the catalogue app with equivalent arguments and behavior. Publish a catalogue-only image change plus an app addition and removal to `main`; another update changes available commands and subsequent launches without changing the executable's checksum. A failed update retains the prior installation, and running services and their data remain intact.
