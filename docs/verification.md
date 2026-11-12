# Native launcher verification record

## Hosted native verification

On 2026-11-12, local verification at commit `b8ad8c0fb7ee6a295221fef1bba01eca2e00594b` passed `go test -race ./...` and `go vet ./...` with module network access disabled. All five supported/preview targets built with `CGO_ENABLED=0`, and their locally generated checksums verified. The cached-image Redis integration scenario also passed on Ubuntu 26.04.1 WSL2 amd64 with Docker Desktop Engine 29.9.3/API 1.56. These local assets are not downloaded release artifacts, and cross-compilation does not verify native execution.

The GitHub Actions run for PR #20, commit `6b9f9878d9de45b8a511d5097ad4c72d458a88f0`, passed the native launcher jobs on `ubuntu-26.04` amd64, `ubuntu-26.04-arm` arm64, `macos-26` arm64 and `windows-2025` amd64, as well as the verify and Renovate jobs: [run 43247514060](https://github.com/BabisK/idapps/actions/runs/43247514060). These are native launcher and fake-API checks; they do not verify real Docker engines or image compatibility. Windows ARM64 and the separate real-engine checks remain gaps.

On 2026-11-12, the opt-in Redis integration scenario passed on Ubuntu 26.04.1 LTS under WSL2 amd64 with Docker Desktop 4.99.0, Engine 29.9.3, API 1.56 and Linux amd64 containers. It used an already cached Redis image and isolated test-owned resources, checked client access, refused cleanup while running, and verified data persistence across restart. This is WSL2 with Docker Desktop, not a native Ubuntu Docker Engine result. The initial attempt failed because the test launcher was named `idapps-spec-verification`; repeating with the required `idapps` basename passed.

2026-11-10, local development run:

- Renovate 44.226.2: `renovate-config-validator --strict renovate.json` passed; a local Renovate extraction discovered all 22 image objects, and the Go extraction test covered all six digest field orders. An actual Renovate update/write-back remains untested.
- Go 1.26.8 on WSL2 Linux amd64: `go test ./...` and `go vet ./...` passed. All five approved OS/architecture targets cross-compiled with `CGO_ENABLED=0`. Cross-compilation does not establish runtime or image compatibility.
- Windows amd64, Windows build 26300, Windows PowerShell 5.1.26100.9818: the catalogue, dispatch, fake-engine, storage and update/recovery test binaries passed. An executable symlink made by Go dispatched as an app and reported the expected missing-cache error. PowerShell's `New-Item -ItemType SymbolicLink` itself reported that administrator privilege was required; Go's symlink operation succeeded on this machine.
- Windows amd64, PowerShell 7.6.9 (official portable archive; SHA-256 verified against the GitHub release): an isolated Redis service and client passed SET/GET across restart, tailed logs, running-clean refusal, stop and clean against the same Docker engine.
- Windows amd64 native ConPTY with Docker Engine 29.9.3: an interactive Redis client returned PONG and exited after Ctrl+C. A separate ephemeral TTY command reported an initial size of 25 rows × 80 columns and, after ConPTY resize, 30 × 100. These checks do not establish console restoration or cancellation behavior on other platforms.
- Release workflow: `actionlint` passed, and a local build of all five final-format binaries passed checksum verification. The hosted publication job has not run. Apple Developer ID signing and notarization are deferred, and candidate macOS builds are not Developer ID signed or notarized.
- WinGet 1.29.380 validated a temporary portable singleton manifest with the local executable SHA-256. Its URL was deliberately non-downloadable; installation was not tested and nothing was published.
- Windows amd64 with Docker Desktop Engine 29.9.3, API 1.56, Linux amd64 containers: an isolated Redis service and `redis-cli` returned `PONG`; a second isolated service preserved a value across stop/restart, produced tailed logs and refused `clean` while running. Both tests stopped and cleaned their own uniquely named resources. A further isolated test sent `PING` through redirected native stdin, closed it, received `PONG` on stdout, and exited successfully; it also cleaned its service resources. An ephemeral `/bin/cat` command round-tripped native redirected bytes `00 01 7F 80 FF 0D 0A` and exited 0; this does not assert PowerShell 5 pipeline byte preservation. A first attempt from a WSL UNC working directory failed because Docker Desktop could not access the WSL distribution mount service; repeating from a native Windows temporary directory passed.

These local checks do not cover the full acceptance matrix. Ubuntu 26.04 native engines on both Linux architectures, Windows ARM64, real Docker on macOS Apple silicon, all 22 images, POSIX interactive TTY/resize/cancellation, console restoration, comprehensive fault injection and downloaded release assets remain unverified. WinGet distribution, including installation testing, and Apple Developer ID signing and notarization are deferred. No release or WinGet package was published as part of these checks.

The observed `psql` failure to resolve a host UID without an explicit PostgreSQL username is deferred to a separate spec at the user's direction.

## Renovate JSONata manager

The catalogue manager uses JSONata to extract image objects independently of field order. Local tests using Renovate 44.226.2 extracted all 22 production images and exercised its replacement function against temporary files. Tag-only and digest-only updates passed, as did combined tag-and-digest updates in four of six field orders and selected repeated-image cases. Combined tag-and-digest updates failed with `update-failure` when `digest` was the first image field. This limitation is accepted for now; an actual hosted Renovate update has not been verified. The earlier Go regex extraction test was removed because it does not exercise JSONata.

## Anonymous registry manifest check

2026-11-10 at approximately 21:47–21:49 UTC, anonymous HTTPS requests queried the 16 unique registry references used by the 22 catalogue apps. The table reflects the subsequently authorized MySQL and kubectl image changes; the kubectl index was checked on 2026-11-11. These checks read manifest indexes only; they did not pull layers or run images. A listed architecture is an advertised Linux image, not a runtime compatibility result. Tags are mutable, especially `helmfile:canary`; digests below identify the index observed during this check, not new catalogue pins. `arm64/v8` is listed as arm64. A 404 means the reference was not anonymously resolvable at that time and does not distinguish deletion from access restrictions.

| Apps | Reference | linux/amd64 | linux/arm64 | Observed index digest or result |
| --- | --- | --- | --- | --- |
| `helm` | `alpine/helm:4.3.1` | advertised | advertised | `sha256:e3f8d5c462d75470fbb1fce79ad5c20063657096d1f98c0fcea869e2600e2e5e` |
| `aws` | `amazon/aws-cli:2.39.7` | advertised | advertised | `sha256:bba262340b0bc7cbf3a8a48612531263b56f4b280c4f036a07816af1b271bda0` |
| `kubectl` | `alpine/kubectl:1.37.2` | advertised | advertised | `sha256:1a7fc3308a8eee1c8bd177930fc521088c6dc64ad8b68c88319151fd87d79deb` |
| `pgadmin4` | `dpage/pgadmin4:9.19` | advertised | advertised | `sha256:3591b3d02910dcd817dec6fcaa681c278ba6966945283dd0ea0042731c330c83` |
| `etcd`, `etcdctl` | `gcr.io/etcd-development/etcd:v3.7.3` | advertised | advertised | `sha256:0d27b9478551f431f82d1e0ddb6c87ad07ebae8b904c637f3a99213349523910` |
| `helmfile` | `ghcr.io/helmfile/helmfile:canary` | advertised | advertised | `sha256:f42c8e93e9590218dde0fb875f7f847254358ab3d036f6205494e771ebc6d510` |
| `go` | `golang:1.24.6-bullseye` | advertised | advertised | `sha256:2cdc80dc25edcb96ada1654f73092f2928045d037581fa4aa7c40d18af7dd85a` |
| `mariadb-server` | `mariadb:11.3.2-jammy` | advertised | advertised | `sha256:e101f9db31916a5d4d7d594dd0dd092fb23ab4f499f1d7a7425d1afd4162c4bc` |
| `memcached` | `memcached:1.6.17-alpine3.16` | advertised | advertised | `sha256:50cc3f190f3b4c13e6875e3fa4eb7ce86a756ce00ffe34c243aff0756d0ebef7` |
| `mysql`, `mysql-server`, `mysqldump` | `mysql:8.0.46` | advertised | advertised | `sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b` |
| `nats-server` | `nats:2.9.8-alpine3.16` | advertised | advertised | `sha256:406dffdd6635f552a4102d89dea2ec47c566a4a3c77a55f174cf7e01163cb935` |
| `nats` | `natsio/nats-box:0.20.2` | advertised | advertised | `sha256:5162fca0f56ebe6f78e612e1ded143ce978e088393476033020e6251358dba6f` |
| `phpmyadmin` | `phpmyadmin:5.2.3-apache` | advertised | advertised | `sha256:f54a37b908b26caa7032c4eb0c7f3495fdd90208a06eed9c4b2c8e691dd1662c` |
| `pg_dump`, `postgres`, `psql` | `postgres:15.1-alpine3.16` | advertised | advertised | `sha256:cc663286e63810373bdfc91a5ed24b772447fb5282d44cfe19f820b6b81197f2` |
| `redis-stack` | `redis/redis-stack:7.4.0-v3` | advertised | advertised | `sha256:c2019e98fd5abce4dd11feec004de44d1709d2366a6efa5ffb2bd0daf8f9c6a4` |
| `redis`, `redis-cli` | `redis:7.0.5-alpine3.16` | advertised | advertised | `sha256:2700d5097763fda285c463f4eefc3d0730a2df2a9d48e66707b19d5a5e5f23d4` |

### Image replacement evidence

Anonymous registry inspection at 2026-11-10 21:48 UTC found the following same-version candidates. The MySQL change was subsequently authorized and applied to the three MySQL apps; the user later selected `alpine/kubectl:1.37.2` instead of the Rancher candidate. Tags can change.

- `rancher/kubectl:v1.23.6`: Docker Hub index `sha256:cb1ba117fd8cc5c5c46219ac4da346c6316488062e6b6c39bed656670b610ff0` advertised linux/amd64 and linux/arm64. The public configs specify `/bin/kubectl` as entrypoint, `help` as default command and `kubectl` as image user; they do not set `HOME` or `KUBECONFIG`. On Windows amd64 with Docker Engine 29.9.3 and PowerShell 7.6.9, the client reported v1.23.6 and, with `KUBECONFIG=/.kube/config`, read the expected context from a synthetic mounted kubeconfig. Linux/macOS host UID behavior, Windows permissions with real user config, and ARM64 execution remain untested.
- `mysql:8.0.46`: Docker Hub index `sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b` advertised linux/amd64 and linux/arm64/v8. Both platform configs declare `docker-entrypoint.sh`, `mysqld` and `/var/lib/mysql`, matching the corresponding fields in the previous Debian variant. The replacement uses a different image variant, has `/` as working directory and an additional `MYSQL_SHELL_VERSION` environment variable. On Windows amd64 with the same Docker engine and shell, a server and mysql client connected and reported 8.0.46; a synthetic database on a new uniquely named volume survived stop/restart, clean was refused while running, and test resources were cleaned. The previous `mysql:8.0.46-debian` index (`sha256:9382e4f6f7f013231049ab854de9d0810594d028f4237b67102acc7ade8e3a99`) advertised amd64 only. Existing database compatibility, mysqldump behavior and ARM64 execution remain untested; no database migration was attempted.

## Native platform CI coverage planned

The `native launcher` workflow defines native GitHub-hosted `ubuntu-26.04` and `ubuntu-26.04-arm` jobs. They check OS and CPU identity, build the launcher, run the offline Go suite and perform launcher smoke checks. A passing hosted run is recorded above.

Real-engine and image-compatibility verification is performed separately on available native machines; results and untested targets remain recorded here. The opt-in local Redis integration test requires its image to be cached beforehand and does not intentionally pull an image. Default automated tests use local fixtures or fake services rather than GitHub or external network services.

The workflow also defines native `macos-26` arm64 and `windows-2025` amd64 jobs for Go tests, vet and launcher smoke checks; the Windows job checks both PowerShell 5.1 and PowerShell 7. A passing hosted run is recorded above. Those jobs do not claim or attempt Docker Desktop coverage.

## Selected kubectl image

Docker Hub metadata and registry configuration for `alpine/kubectl:1.37.2` advertise Linux amd64 and arm64. Both configurations declare `/usr/local/bin/kubectl` as entrypoint and `--help` as the default command. The catalogue sets `KUBECONFIG=/.kube/config` for the existing mount. On Windows amd64 Docker Desktop, `kubectl version --client` reported v1.37.2. The same image read a synthetic mounted kubeconfig and returned its expected context in Docker Desktop on Windows amd64. This image check used the Docker CLI, not the launcher. ARM64 execution and Kubernetes cluster compatibility remain unverified. Kubernetes supports kubectl within one minor version of the API server.

## Frozen I/O matrix audit

The following local automated checks ran on WSL2 Linux amd64 with Go 1.26.8. The Docker responses in these checks are fakes; they do not establish native Docker compatibility.

| Matrix area | Passing automated evidence | Remaining limits |
| --- | --- | --- |
| Update | The activation, failed transaction and catalogue change scenarios cover selected journal/link/activation faults, rollback, add/remove/image changes, and preservation of the binary, a foreign file and a retired service record. | Faults at every transaction boundary, concurrent interference and recovery on every target OS are not exhaustively covered. |
| Dispatch | The command dispatch scenarios cover app names, direct invocation, `run` and argument preservation; command execution scenarios cover the Docker API boundary and exit status; CLI scenarios cover missing cache and retired-service logs. | End-to-end stream and exit equivalence for all 22 apps through every invocation form and shell precedence remain unverified. |
| Docker | Tests cover rejected remote/missing contexts without fallback or credential disclosure, engine permission/wrong-OS failures, cached-image use, anonymous pull failure and cancellation. | Missing local engine and registry/network behavior are not exhaustively tested on each target; credential-helper absence is established by implementation and request checks, not by every runtime combination. |
| Service | Fake-engine tests cover owned creation, foreign container/volume refusal, in-use volume refusal, incomplete inspection and update identity checks while logs follow; the real Windows Redis checks above cover a representative lifecycle. | Retired service management with real Docker, engine-switch recovery, interrupted creation and every service/image remain incompletely covered. |
| Streams | A real POSIX pseudo-terminal with a fake engine verifies TTY selection, initial size, raw-mode entry and terminal restoration after normal exit, start failure and cancellation. Fake-engine tests cover separate binary stdout/stderr, argv, exit status, cancellation interrupt and owned-container cleanup. | Real POSIX Docker interactive sessions, signal delivery, resize, large output and broken downstream pipes remain unverified; Windows terminal restoration is not established. |
| Storage | Cache deletion/re-download, owned link repair, moved-binary reconciliation, retargeted-link refusal, rollback and saved service records are covered by update tests. | A full binary-replacement and recovery fault matrix across separate filesystems and every target OS remains unverified. |

On 2026-11-11, the updated fake-Engine and update test binaries also passed on Windows amd64 build 26300 under Windows PowerShell 5.1.26100.9818. The POSIX pseudo-terminal tests are not part of that Windows run.

The default `go test ./...` run skips the opt-in real-Docker Linux integration test when `IDAPPS_DOCKER_INTEGRATION` is not set. The local checks in this section are separate from the hosted results recorded above.

## BDD suite migration

The Go tests use Ginkgo v2 and Gomega, grouped by behavior and scenario with individually reported table cases. `go test -race ./...`, `go vet ./...`, and three randomized Ginkgo race-enabled runs pass locally on Linux amd64. The restructured Windows amd64 and macOS arm64 test binaries cross-compile but have not been executed. Before the restructuring, Windows amd64 suites passed under PowerShell 5.1 and 7.6.9, with Linux-only and opt-in integration cases skipped. The real-Docker integration spec remains opt-in and was skipped locally. Resolving Ginkgo/Gomega dependencies upgraded `golang.org/x/term`; the earlier real Windows TTY checks predate that dependency change and have not been rerun against it.

After adding Cobra and Viper, the Linux amd64 race-enabled suite and vet pass and all five launcher targets cross-compile. The resulting binaries have not been revalidated with a real Docker engine or executed on native Windows or macOS.

After moving execution into Cobra root and subcommand files, the Linux amd64 race-enabled suite and vet pass, all five launcher targets cross-compile, and a Linux binary built with the release version flag reports the injected version. Real-Docker and native Windows/macOS execution remain unverified for this revision.

After switching to `google/jsonschema-go`, the Linux amd64 race-enabled suite and vet pass and all five launcher targets cross-compile. A local comparison with the previous validator agreed on the production catalogue and representative schema failures and port boundaries; it was not exhaustive. Real-Docker and native Windows/macOS execution remain unverified for this revision.

The SemVer compatibility and build-version suites cover pre-release precedence, ignored build metadata, tagged and untagged builds, dirty state and missing metadata. A native Linux build from the current tagless dirty checkout reported `0.0.0` with the full commit and `dirty` metadata; a build with a release version injected reported `2.0.0`. Native Windows/macOS runtime checks and hosted workflow runs for this change remain outstanding.

## Expanded automated acceptance coverage

The Ginkgo suite uses GoMock for Docker HTTP requests through the Docker SDK. It checks create requests for all 22 catalogue apps, image reuse and anonymous pulls, foreign or in-use resources, engine and container identity, interrupted creation, and retired-service cleanup. Separate socket-based fakes check binary stdin and EOF, large distinct stdout/stderr streams, exit status and closed downstream output. Executable-level tests exercise all app links and direct invocation, missing links, command shadowing and operation without Git, Go or the Docker CLI on PATH. Update tests cover pinned-commit downloads, failures, lock cancellation, foreign journals and rollback at each current app-link boundary.

These are local tests with mocked Docker responses; create-request tests intentionally stop before container start. POSIX shell precedence was run locally; PowerShell 5.1 and 7 precedence scenarios require Windows execution. They do not replace the required real-engine, hosted platform, download or full platform I/O and mount checks in the acceptance matrix. WinGet distribution is deferred.
