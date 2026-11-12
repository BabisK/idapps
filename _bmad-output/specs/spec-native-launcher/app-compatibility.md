# Existing app coverage and verification

The behavior of all 22 root wrappers at commit `cdcc384` must be ported: 12 commands and 10 services. The native implementation removes those wrappers. The initial values are captured in [catalogue.example.json](catalogue.example.json); implementation should reconcile later repository changes before publishing the production catalogue. Image pins are baseline data except for the user-approved MySQL tag `8.0.46` and `alpine/kubectl:1.37.2`; do not independently upgrade other dependencies.

## Approved release and verification matrix

Build all targets from the same Go codebase.

| Go build target | Status | Required native verification |
| --- | --- | --- |
| `windows/amd64` | Supported | Native launcher/fake-API checks on `windows-2025`; real Docker checks on compatible Windows with PowerShell 5.1 and a supported PowerShell 7 release. |
| `windows/arm64` | Preview initially | Native Windows ARM64 with compatible local Desktop/Linux containers; PowerShell 5.1 and a supported PowerShell 7 release. Record the shell architecture used. |
| `linux/amd64` | Supported | Native launcher/fake-API checks on GitHub-hosted `ubuntu-26.04`; real Docker checks separately on compatible Ubuntu 26.04. |
| `linux/arm64` | Supported | Native launcher/fake-API checks on GitHub-hosted `ubuntu-26.04-arm`; real Docker checks separately on compatible Ubuntu 26.04. |
| `darwin/arm64` | Supported | Native launcher and fake-API tests on GitHub-hosted `macos-26` (Apple silicon); record real Docker Desktop/Linux-container testing separately when a compatible engine is available. |

Linux support is distribution-independent: build without a libc dependency and document the kernel/runtime minimums of the selected Go toolchain and compatible local Docker Engine. Ubuntu 26.04 is the required Linux verification target, not an exclusive distribution allowlist. macOS support requires a version supported by the selected Go toolchain and Docker Desktop; native CI uses `macos-26` and records its exact version. No Intel macOS artifact is in scope. Windows retains the Microsoft-supported-OS policy subject to compatible local Docker prerequisites.

Each target needs native launcher smoke tests and verification of image compatibility for all 22 apps on its Linux-container architecture; cross-compilation alone does not establish runtime support. Record unavailable runners, preview environments and failed image-compatibility checks as verification gaps rather than claiming a pass or silently dropping apps. Windows ARM64 remains preview initially; promoting it requires a verified supported Docker stack and a deliberate support-policy change.

Automated tests use local fixtures, mocked HTTP or local fake engines and must not require Internet access or GitHub availability. Native runner smoke tests remain automated. Perform real-engine and image-compatibility checks separately as manual verification when the required machines and images are available; record the target, versions and results, and keep missing checks visible as verification gaps. Opt-in real-engine test runs require their images to be present locally and must not pull them.

## Wrapper replacement and existing installations

Remove the existing runtime scripts as part of the native implementation:

- All 22 root POSIX app wrappers listed in the command/service inventory below.
- `lib/clean.sh`, `lib/logs.sh` and `lib/stop.sh`.
- The `powershell/` app entry points and `dispatch.ps1`.
- The wrapper-specific `tests/powershell-tests.ps1`, replacing its relevant coverage with native launcher/API tests.

Keep no compatibility shell/PowerShell app shims. Preserve invocation from the approved shells, including PowerShell 5.1 and supported PowerShell 7, through the native executable and its symlinks. Unrelated build/spec tooling and historical specification artifacts are not app wrappers and are outside this removal.

Update README installation, command examples and PATH guidance for native downloads and `idapps update`. Refresh repository agent context to reflect Go, the native runtime and catalogue, removing obsolete wrapper-location/POSIX-runtime instructions. Update CI and replace shell-variable Renovate discovery with `apps.*.image` discovery; keep the existing no-dashboard policy. The catalogue is the application-definition source after migration.

Existing users manually replace their old repository-root or `powershell` PATH entries with the native app-bin directory, without changing precedence of unrelated commands. An older checkout may continue to shadow a bare name until its PATH entry is changed; `idapps NAME …` selects the native app meanwhile. Repository removal and installation/update must not scan for or delete users' old copies or edit their shell profiles automatically.

Existing unowned containers and volumes are neither adopted nor deleted. Report an exact name conflict without modifying the resource; legacy-data migration remains a user-managed operation. Catalogue removals delete only owned app links. Native service records remain available for `idapps NAME stop`, `logs` and applicable `clean`, even after the app leaves the catalogue. Catalogue updates never migrate databases, restart services or delete their data.

## Commands

All commands are ephemeral, forward arguments, use host networking, use host UID:GID on Linux/macOS and image-default users on Windows. Mounts are writable, matching existing behavior.

| Command | Image repository | Container command/entrypoint | Host mounts and working directory |
| --- | --- | --- | --- |
| `aws` | `amazon/aws-cli` | Image defaults | Home `.aws` → `/tmp/.aws`; cwd → `/aws`; inherit image workdir. Set AWS config/credentials locations under `/tmp/.aws`. |
| `etcdctl` | `gcr.io/etcd-development/etcd` | Entrypoint `/usr/local/bin/etcdctl`; clear image CMD | cwd → `/etcd`; workdir `/etcd`. |
| `go` | `golang` | CMD prefix `go` | cwd → `/src`; workdir `/src`. |
| `helm` | `alpine/helm` | Image entrypoint | cwd → `/helm`; workdir `/helm`; Helm configuration mounts below. |
| `helmfile` | `ghcr.io/helmfile/helmfile` | Entrypoint `/usr/local/bin/helmfile`; clear image CMD | cwd → `/helmfile`; workdir `/helmfile`; Helm configuration mounts below. |
| `kubectl` | `alpine/kubectl` | Image entrypoint | Home `.kube` → `/.kube`; set `KUBECONFIG=/.kube/config`; no cwd mount; inherit image workdir. |
| `mysql` | `mysql` | CMD prefix `mysql` | cwd → `/mysql`; workdir `/mysql`. |
| `mysqldump` | `mysql` | CMD prefix `mysqldump` | cwd → `/mysql`; workdir `/mysql`. |
| `nats` | `natsio/nats-box` | Entrypoint `/usr/local/bin/nats`; clear image CMD | cwd → `/nats`; workdir `/nats`. |
| `pg_dump` | `postgres` | CMD prefix `pg_dump` | cwd → `/postgres`; workdir `/postgres`. |
| `psql` | `postgres` | CMD prefix `psql` | cwd → `/postgres`; workdir `/postgres`. |
| `redis-cli` | `redis` | CMD prefix `redis-cli` | cwd → `/redis`; workdir `/redis`. |

Helm mounts are home `.kube` → `/.kube`, `.helm` → `/.helm`, `.config/helm` → `/.config/helm` and `.cache/helm` → `/.cache/helm`. Create these directories if missing, as for kubectl's `.kube`. Create the missing `.aws` directory to preserve Docker's legacy directory-mount behavior. The launcher does not rewrite kubeconfig/AWS files or install credential helpers referenced within them; the container must be able to resolve those references itself.

## Services

All service ports are TCP and retain the existing all-interface publication default (`0.0.0.0`); these are local development settings. Every service supports start/stop/logs. Only rows with named volumes support clean. Ports shared by alternate services remain intentional conflicts, not catalogue validation failures.

| Command | Container name | Host → container ports | Named volume → target | Additional defaults |
| --- | --- | --- | --- | --- |
| `etcd` | `etcd` | 2379→2379, 2380→2380 | `etcd` → `/etcd-data` | Entrypoint `/usr/local/bin/etcd`; listen/advertise client URLs `http://0.0.0.0:2379`. |
| `mariadb-server` | `mariadb` | 3306→3306 | `mariadb` → `/var/lib/mysql` | `MARIADB_ROOT_PASSWORD=root`. |
| `memcached` | `memcached` | 11211→11211 | — | Image defaults. |
| `mysql-server` | `mysql` | 3306→3306 | `mysql` → `/var/lib/mysql` | `MYSQL_ROOT_PASSWORD=root`. |
| `nats-server` | `nats-server` | 4222→4222, 6222→6222, 8222→8222 | — | Image defaults. |
| `pgadmin4` | `pgadmin4` | 8080→80 | `pgadmin4` → `/var/lib/pgadmin` | Email `user@pgadmin.org`; password `pgadmin`. |
| `phpmyadmin` | `phpmyadmin` | 8082→80 | — | `PMA_ARBITRARY=1`. |
| `postgres` | `postgres` | 5432→5432 | `postgres` → `/var/lib/postgresql/data` | User/password `postgres`. |
| `redis` | `redis` | 6379→6379 | `redis` → `/data` | CMD `--save 60 1`. |
| `redis-stack` | `redis-stack` | 6379→6379, 8083→8001 | `redis-stack` → `/data` | Image defaults. |

## Implementation acceptance matrix

These are required implementation checks, not tests claimed to have run while writing the spec. Use isolated engine resources and user directories. Do not expose mounted credentials in test output.

| Area | Evidence required |
| --- | --- |
| Distribution | Native Go build for every declared release target; final-asset checksums; standalone Windows EXE and raw Linux/macOS downloads; native macOS binary checks. WinGet distribution and Apple Developer signing and notarization are deferred. Verify manual setup and first update without Git, Go or Docker CLI on PATH, including paths with spaces/Unicode. |
| OS support | Verify the approved matrix above; document Go/Docker/OS versions, Linux minimums, the Microsoft-supported Windows policy, macOS compatibility and untested supported combinations. Build the Linux release without a libc dependency. |
| Full app inventory | Validate every one of the 22 definitions against schema and semantic rules and compare Docker create/attach/start/log/stop/volume requests with this inventory. Check each embedded image against its app's source and retain intended port conflicts. Reject the former top-level images map, string image references and management-name collisions. |
| Dispatch and precedence | Every app through its link, full link path, `idapps NAME …` and `idapps run NAME …` on Windows, Linux and macOS; identical argv, lifecycle and exit behavior; direct invocation with its link absent and with a shadowing alias/function/native executable; full launcher path when idapps is shadowed; reserved management names and app `--help` forwarding; Windows case/`.exe`; Linux/macOS invocation basename. |
| First/update | Empty installation, repeat update, version-only change, add/remove app, missing-link repair and an unchanged executable hash across catalogue-only changes. No engine needed; no Docker mutation during update. |
| Update failure/recovery | Download failure, branch movement, oversized/truncated/duplicate-key/unknown-field JSON, unsupported schema/minimum launcher, case/reserved-name/path/image-reference errors, link privilege denial, foreign/retargeted paths, concurrent updates and fault injection at every transaction boundary. Prior generation recovers without unrelated changes. |
| Cache and durable state | Verify OS cache/configuration defaults and absolute XDG overrides, Linux ~/.config ownership/recovery records, Windows APPDATA records versus LOCALAPPDATA cache, macOS Application Support records versus Caches, relative-override errors, separate filesystems and an executable directory that is not writable. Reject copied/roamed foreign-host ownership journals. Clear the catalogue cache before/during update and after activation: ownership, locks and service records survive; launches request update; explicit service management remains possible; update safely restores catalogues/links. Replace or relocate the binary without losing durable state, and reconcile only owned links when its path changes. |
| Offline and drift | Run valid cached apps without GitHub; absent-image pull errors; corrupt cached catalogue/durable state; changing a tag versus a digest; run/list behavior before first update and after cache clearing; selected remote/missing contexts rejected. |
| Image policy | A cached image causes no pull; an absent image is pulled anonymously on launch with progress on stderr. Catalogue update makes no Docker/image calls. A changed tag/digest is selected on the next launch; an unchanged mutable tag is not refreshed. Authentication-required failures are actionable and do not invoke registry credential helpers. |
| Arguments | Empty values, spaces, nested quotes, Unicode, leading dashes and shell metacharacters at the native process/API boundary. Test PowerShell invocation separately and record losses introduced by its native argument handling. |
| I/O | All-terminal interactive session, stdin pipe/EOF, stdout `> file`, stderr redirection, independent stream bytes, mixed redirections, large output, closed downstream pipe, exit statuses, Ctrl+C, terminal restore and resize. Compare native byte fixtures before/after; distinguish PowerShell text transformations from launcher behavior. |
| Real engines | Windows PowerShell 5.1 and a supported PowerShell 7 version with local Desktop/Linux containers; Ubuntu 26.04 on both architectures with local Engine; real macOS Apple-silicon Docker testing remains a separate verification gap when no compatible engine is available. Verify a representative client/service connection (e.g. PostgreSQL/psql or Redis/redis-cli), interactive and redirected runs, logs, stop/restart data persistence and clean refusal while running. Record exact versions. |
| Mounts | cwd/home/config paths, spaces/Unicode/drive roots, missing-directory creation, permission/sharing failures, Linux/macOS UID:GID, macOS Desktop file sharing and Windows image-default user. Check AWS/Kubernetes/Helm mounts and kubectl's intentional lack of cwd mount. |
| Service ownership | Foreign/legacy same-name container and volume refused; exact-name matching; interrupted creation recovered by labels; engine switch; running image/config update; retired service stop/logs/clean; identity-change rejection; unrelated/in-use volume preserved. |
| Maintenance | CI rejects incompatible catalogue changes before merge to main; Renovate discovers embedded app image objects and mechanically updates every targeted occurrence; all former app wrappers, shared runtime helpers and wrapper-specific tests are removed; native tests cover their supported behavior; README/agent context/CI have no active dependency on those deleted scripts. |

End-user documentation must demonstrate plain-binary download, migration from old checkout/PATH entries without changing user data, manual executable permissions where needed, PATH append, first update, direct `idapps NAME …` and its `run` alias for every app, full launcher/link paths for conflicts, Windows symlink prerequisites, macOS first-launch instructions, missing engine errors, Desktop host networking, catalogue-only updates, binary release replacement/relocation, cache and configuration-record paths, cache clearing/re-download, retired service cleanup and safe binary exports from older PowerShell.
