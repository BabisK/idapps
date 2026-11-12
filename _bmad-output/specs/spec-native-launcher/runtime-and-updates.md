# Native runtime and catalogue updates

This companion specifies CAP-1, CAP-3, CAP-4 and CAP-5. Approved release formats and OS-specific storage paths are in `distribution-and-storage.md`; the catalogue/interface contract is in `catalogue-format.md`; wrapper migration is specified in `app-compatibility.md`. The image-pull policy below is part of the required runtime contract.

## Installation and dispatch

Distribute one executable per OS/architecture from one Go codebase: `idapps` on Linux/macOS and `idapps.exe` on Windows. Follow the platform-specific binary formats and setup in `distribution-and-storage.md`; no catalogue or app links are bundled. Cache, durable state and generated links use their declared per-user locations, independently of where the executable was downloaded or installed.

Record the per-user installation UUID, active catalogue commit/hash reference, link ownership and resource snapshots in durable records under the OS configuration directory. Downloaded catalogue generations stay in the cache. Updating from a newly located launcher can reconcile its owned link targets; ordinary invocation cannot silently change that registration. Binary replacement must preserve state and cache, including when a package manager owns the executable path.

`idapps update` creates `psql -> idapps` on Linux/macOS and `psql.exe -> idapps.exe` on Windows in the declared app-bin directory, and likewise for every app. These targets are relative when the binary is colocated and absolute when it is installed elsewhere. Determine dispatch from the invoked basename before resolving the executable symlink; resolving it first can lose the app name. Accept the native `.exe` suffix on Windows and normalize case. A full link path is an explicit IDApps invocation. Do not require a per-app executable build or copied binary. Go documents that [`os.Executable` may return the symlink or its target](https://pkg.go.dev/os#Executable), so it is not alone a reliable dispatch identity.

Document appending the per-user app-bin directory to PATH after existing entries, leaving aliases/functions and earlier executables in precedence. Do not edit PATH or profiles from `update`. A foreign file already in IDApps' app-bin directory is a different kind of conflict: fail the update and name that file. Do not overwrite it or silently omit its app.

On Windows, use native file symlinks and test the ability to create them before activation. The OS needs suitable symlink rights; supported Developer Mode configurations permit unprivileged creation when using the relevant API flag. Otherwise explain the required privilege/elevation, retaining prior state. Never enable Developer Mode, elevate automatically or fall back to another link type. See [CreateSymbolicLinkW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-createsymboliclinkw).

| Invocation | Behavior |
| --- | --- |
| `idapps update` | Download, validate and reconcile the main-branch catalogue and links. |
| `idapps NAME [ARG…]` | Directly dispatch any catalogue app, without shell/PATH lookup or executing its symlink. Arguments after NAME belong to it, including `--help`. Also permit management-only operations on recorded retired services below. |
| `idapps run NAME [ARG…]` | Explicit alias of `idapps NAME [ARG…]`, with identical command/service behavior. |
| `idapps list` | List current apps/kinds and distinguish retired services with retained managed resources. No network or Docker required; report missing catalogue cache and still expose managed service records. |
| `idapps version` | Report launcher version, supported schema version and active catalogue commit if present. |
| `idapps help` / `idapps --help` | Document setup and management usage. No args also shows help. |
| `APP [ARG…]` | Dispatch the named app through its link. A command app gets all arguments literally; a service uses the lifecycle below. |

When invoked as `idapps`, recognize `update`, `run`, `list`, `version` and `help` as management commands; otherwise interpret the first argument as a catalogue app name. Those management names are reserved in catalogue keys. An app-link invocation treats every argument as that app's input instead. `idapps psql --help` reaches psql; `idapps postgres start` reaches the service lifecycle. Direct invocation bypasses any alias, function or executable named like the app and does not require its symlink to exist after a successful catalogue installation. If `idapps` itself is shadowed, invoke the launcher by full path. Unknown app names fail without falling back to an external executable.

Before first update, app dispatch/list reports that no catalogue is installed and instructs the user to run `idapps update`; help/version still work. If a previously installed catalogue cache was cleared, new launches require an update, while explicit stop/logs/clean of recorded owned services remain available from durable resource snapshots. An app removed from the catalogue cannot start through an old manually retained link.

## Fetching and activating a catalogue

The source is the public `BabisK/idapps` repository, branch `main`, path `catalogue.json`. Resolve the branch head through GitHub's HTTPS API, then fetch that exact commit's raw file. Record the commit and SHA-256 of the downloaded bytes; the hash detects local corruption and is not an independent publisher signature. This prevents mixing revisions if `main` changes during a download. No Git installation, checkout, GitHub token or Docker connection is required.

Use HTTPS certificate verification, a finite request timeout and a maximum decoded document size of 5 MiB. Restrict fetching to the fixed GitHub API/raw-content source; do not follow downgrade or arbitrary-origin redirects. Report network, rate-limit, missing-file, validation and minimum-version errors distinctly. A transport failure is not an invitation to accept another source or pretend a cached catalogue is a successful update.

An update has these observable stages:

1. Fetch and validate into a temporary file in the catalogue cache. Do not change the active catalogue, app links or Docker resources.
2. Obtain the installation update lock, recover any prior transaction, then recheck the active generation and current ownership. Preflight directory permissions, symlink support, foreign path conflicts, resource-identity compatibility and the complete add/keep/remove plan.
3. Stage an immutable catalogue generation in cache, and stage the desired ownership manifest and transaction journal in durable state. The journal records the prior generation reference and link targets. Finalize each file within its own destination filesystem; do not rely on a cross-filesystem rename.
4. Reconcile app links under the lock, changing only recorded owned links that still match their recorded targets; create missing desired links only at absent paths. Activate the new generation through the durable active-reference record after the link work succeeds and its cached bytes have been verified. Use replace/rename operations where supported, with explicit recovery for platforms that cannot replace a destination in one operation.
5. Commit the new ownership manifest, finalize the journal and report the source commit and added/removed/repaired/unchanged counts. A repeat update at the same commit performs no unnecessary rewrite, but repairs missing owned links.

The cache, state and app-bin directories are **not** a single atomic filesystem transaction. Dispatch through `idapps` or an existing link waits for an active reconciliation to finish, snapshots a complete catalogue generation, then releases the lock before running an app. In-flight processes retain the definition they started with. Bare-name lookup of a link being added/removed may transiently fail during update; it must never dispatch using a mixed catalogue. Concurrent updates serialize. Service state changes and update planning share sufficient locking to prevent a resource record appearing after the compatibility preflight unnoticed.

On an ordinary failure, roll back modified owned links and the active pointer to the previous generation. On crash/power loss, the next launcher invocation must complete recovery before dispatch or another update. Preserve prior cache generations and the durable journal until recovery succeeds; if permissions or external file changes prevent recovery, stop with the exact affected paths and recovery guidance rather than overwriting foreign content or running a mixed state. If an external cache cleanup removed required generation bytes, recover consistent link/ownership metadata and require `idapps update` for new launches, as specified in `distribution-and-storage.md`. A first-update failure leaves no active partial catalogue. Verify recovery by fault injection at each journal/link/activation boundary on all three host operating systems.

Ownership is proved by the recorded path **and** the current symlink target/type, using no-follow inspection. A foreign regular file, a retargeted link or an unexpected directory cannot be removed by the updater. Deleting an obsolete owned link does not delete its target executable. Never prune unrelated files by name or scan outside managed directories for deletions.

Updating affects metadata and links only: no image pulls, service restarts, resource migration or deletions. Execution with an intact, valid cached catalogue does not contact GitHub. It may still need registry access if the chosen image is absent. Editing the remote catalogue changes the next launch, not a running container.

## Local Docker access

Use the Go Docker Engine client/SDK and negotiate an API version supported by both sides; fail clearly when no compatible version exists. Docker provides [a Go SDK and API negotiation guidance](https://docs.docker.com/reference/api/engine/sdk/). Never shell out to Docker or run catalogue-supplied host commands.

Choose the endpoint in this order: nonempty `DOCKER_CONTEXT`; otherwise `DOCKER_HOST`; otherwise the active context in the local Docker configuration; otherwise the platform default. Read Docker config/context files directly, respecting `DOCKER_CONFIG`; a Docker CLI installation is unnecessary. Honor local contexts pointing to Linux Engine, rootless Engine or Desktop, including per-user Unix sockets on Linux and macOS. On macOS, use Desktop's local context/socket without requiring a system-wide `/var/run/docker.sock` symlink or installed Docker CLI. Docker documents [Apple-silicon support, supported macOS versions and per-user installation](https://docs.docker.com/desktop/setup/install/mac-install/). Accept only local Unix sockets and local Windows named pipes. Reject TCP/HTTP/SSH endpoints, nonlocal named pipes, missing selected contexts and malformed configuration without silently falling back. Docker [contexts contain endpoint information independently of invocation](https://docs.docker.com/engine/manage-resources/contexts/).

Probe the chosen engine and require Linux containers. Distinguish missing/stopped/unreachable engine, access denied, unsupported endpoint, wrong container OS and API mismatch. Include the selected endpoint/context and an actionable next step; avoid dumping credentials or full environment contents. Do not install, launch or reconfigure Docker automatically.

Use the cached image when present; otherwise pull its tag/digest anonymously, consume the pull response to detect registry errors, and report progress on stderr. Missing tags, unsupported image architecture, authentication required and network failures are explicit errors. Updating a mutable tag in place upstream is not an automatic image refresh; a different tag/digest in the catalogue selects a different reference. Preserve an image-default entrypoint/CMD when the catalogue says to inherit it.

Commands retain host networking. Docker Desktop requires host networking to be supported and enabled; do not switch to bridge silently or change localhost arguments. Services retain published ports. Docker documents [host networking on Linux and its Desktop prerequisites](https://docs.docker.com/engine/network/drivers/host/). Check a representative client/service connection on each platform; a successful container create alone does not prove connectivity.

Resolve bind sources against the caller's working/home directories using platform path APIs. On Windows, translate drive/UNC paths into forms accepted by the local Desktop engine, and report unsupported sharing/path cases. On macOS, preserve native `/Users/...` paths and check Desktop file-sharing access. Test spaces, Unicode, drive roots and configuration paths. Linux/macOS commands use host UID:GID; Windows commands and all services use image-default users. This can change ownership of files written to mounted directories and must be documented.

## Arguments, terminal and streams

Forward the process's parsed argument array to Docker's argv fields without joining it into a command line or evaluating shell syntax. Preserve empty arguments, quotes, whitespace and Unicode as received. PowerShell may alter values before starting a native executable; test/document those cases separately instead of claiming the launcher can reconstruct lost values.

Inspect the native stdin, stdout and stderr handles. Allocate a TTY only when all three are terminals; if any is redirected or piped, use non-TTY mode. In TTY mode, attach before starting the container, establish terminal size, enter/restore terminal mode, relay input, propagate resize and deliver interrupts once. Docker TTY mode combines output streams; separate stderr is guaranteed in non-TTY mode only.

In non-TTY mode, attach before start, copy input bytes, demultiplex Docker's stdout/stderr framing and copy payloads unchanged to their respective native handles. Forward EOF without truncating output, drain both output streams, obtain container exit status before removal and handle closed downstream pipes without hanging. Do not add banners or image-pull progress to app stdout. Interactive Ctrl+C, noninteractive cancellation and POSIX termination signals on Linux/macOS must stop/interrupt the attached command appropriately, clean up its ephemeral container and restore the terminal on success, failure and cancellation; forcibly killing the launcher cannot guarantee immediate cleanup.

Pass through the container's normal exit code. Use exit `2` for launcher usage/validation errors and `125` for launcher/update/engine failures before an app status is available; errors identify their source in stderr. Cancellation without an available app exit status returns `130`. Service lifecycle success returns `0`; failure is nonzero. Capture exit status reliably even when auto-removal is enabled, and remove only the ephemeral container created by this invocation.

The launcher's byte guarantee is between its native handles and Docker. PowerShell versions before 7.4 can decode/re-encode native output during redirection or pipelines; text cmdlets can also transform data. This remains true even without `docker.exe`. Use a tool's file option on a mounted directory (for example `pg_dump -f`) for binary exports in Windows PowerShell 5.1. Microsoft's [redirection documentation](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_redirection?view=powershell-7.5) identifies the native-byte behavior introduced in 7.4. Tests must separate a native-handle regression from a caller-shell limitation.

## Services, ownership and catalogue changes

| Operation | Contract |
| --- | --- |
| No args / `start` | Create/start the named detached service with its configured ports, environment and volumes. Enable automatic removal on exit, retaining named volumes and ownership records. A running instance, conflicting foreign resource or occupied port is an error. Report the actual published endpoints on success. Extra arguments are usage errors. |
| `stop` | Stop and remove the owned container, retaining named volumes. Handle an owned stopped container left after a failure. Missing container is a clear nonzero error. |
| `logs [-f] [-t N]` | Display stdout/stderr of a running owned container; follow if requested. `N` is a nonnegative integer, default all available logs. Missing/invalid tail values fail. Unknown options warn and are ignored as in the legacy helper; `--` terminates option parsing and is valid at the end. Other operands fail. Interrupting follow stops only log viewing. |
| `clean` | Offered only for services with named volumes. Refuse if any relevant container is running or any target volume is in use/unowned. Remove only recorded owned volumes after the service is stopped; never use force to bypass engine protection. Report partial engine failures accurately. |

Create containers and volumes with installation/app ownership labels, and persist actual IDs, engine identity, volume names and the creation definition. Use exact IDs/names plus ownership, never substring filters. No catalogue may supply/replace the launcher's ownership labels. Existing unlabelled legacy resources with the same names cause a clear conflict; do not adopt, relabel, delete or attach them automatically. A new labelled volume may be created only when that name is absent; do not overwrite a foreign volume.

Reconcile a crash between Docker creation and local-record persistence using matching engine/installation/app labels; never infer ownership solely from a friendly name. Switching local engines does not transfer resource ownership: scope records to engine identity and report unavailable recorded resources without acting on similarly named objects on another engine.

Image/config updates affect future starts. Stop/logs/clean for existing resources use their saved creation record, not a newly downloaded mount list. Existing resource identity cannot change through a catalogue edit without first resolving its retained resources. The launcher performs no schema/data migration for database upgrades.

When an app is removed, delete its owned link and mark resource records retired. `idapps NAME stop`, `logs` and applicable `clean` (also through `idapps run NAME …`) remain available for that recorded service; `start` and no-argument invocation fail as retired. `idapps list` exposes this state. Retain records until containers and volumes are gone; re-adding the same name must satisfy the recorded identity checks. This avoids requiring a deleted command link to clean up a still-running service.
