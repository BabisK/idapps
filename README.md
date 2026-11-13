# Instant Docker Apps

IDApps runs developer tools and short-lived services in local Docker containers through one native launcher. The release workflow can build candidate assets and, when explicitly requested, publish them against a matching existing version tag. Platform and image compatibility verification remains incomplete; see [the verification record](docs/verification.md).

The launcher uses the local Docker Engine API and Linux containers. It does not
need the Docker CLI, Git, Go or a shell runtime after installation. Remote Docker
contexts are rejected. Commands use host networking; Docker Desktop must support
and have host networking enabled. Services publish ports on all interfaces by
default. These applications and their default passwords are for local development, not production.

Docker selection honors `DOCKER_CONTEXT`, then `DOCKER_HOST`, then the active
Docker context; only local sockets and pipes are supported. `DOCKER_CONFIG` can
select the Docker configuration directory. IDApps uses these environment
variables and the platform directory settings below; it has no separate settings file.

Download the appropriate binary and SHA-256 checksums from the project's release assets, verify the checksum, place the binary in the app-bin directory, and on Linux or macOS make it executable (`chmod +x idapps`). Append that directory to PATH, leaving existing
entries first. On Windows, use `idapps.exe`; Windows must permit creating file
symlinks, for example through a suitably configured Developer Mode or sufficient
privilege. The launcher does not change those settings. Windows ARM64 is preview.

The supported builds are Linux amd64/arm64, macOS arm64, and Windows amd64. Windows arm64 is preview. Linux binaries use no libc and Go 1.26 requires Linux kernel 3.2 or newer; WSL 1 is unsupported. macOS requires at least macOS 12 for Go 1.26 and a macOS version supported by Docker Desktop. Windows requires a Microsoft-supported Windows version and a compatible local Docker Desktop installation. Docker Engine API 1.30 or newer is required. Refer to the [Go minimum requirements](https://go.dev/wiki/MinimumRequirements) and Docker's installation requirements for your platform.

The catalogue includes `aws`, `etcdctl`, `go`, `helm`, `helmfile`, `kubectl`, `mysql`, `mysqldump`, `nats`, `pg_dump`, `psql`, and `redis-cli`; services are `etcd`, `mariadb-server`, `memcached`, `mysql-server`, `nats-server`, `pgadmin4`, `phpmyadmin`, `postgres`, `redis`, and `redis-stack`. Every app can also be invoked as `idapps NAME ...` or `idapps run NAME ...`. Services support `start`, `stop`, and `logs [-f] [-t N]`; services with named volumes also support `clean`. Stop retains volumes, and clean refuses resources that are running, in use or unowned.

The per-user directories are:

| Purpose | Linux | macOS | Windows |
| --- | --- | --- | --- |
| App links | `$XDG_DATA_HOME/idapps/bin`, default `~/.local/share/idapps/bin` | `~/Library/Application Support/idapps/bin` | `%LOCALAPPDATA%\idapps\bin` |
| Cache | `$XDG_CACHE_HOME/idapps/catalogues`, default `~/.cache/idapps/catalogues` | `~/Library/Caches/idapps/catalogues` | `%LOCALAPPDATA%\idapps\cache\catalogues` |
| Ownership records | `$XDG_CONFIG_HOME/idapps`, default `~/.config/idapps` | `~/Library/Application Support/idapps` | `%APPDATA%\idapps` |

Linux XDG overrides must be absolute. For a shell session, append the app-bin
path as appropriate, for example:

```sh
export PATH="$PATH:${XDG_DATA_HOME:-$HOME/.local/share}/idapps/bin"
idapps update
idapps list
idapps postgres start
idapps psql -h localhost -U postgres
idapps postgres logs -f -t 50
idapps postgres stop
idapps postgres clean
```

In PowerShell, append the app-bin directory and update:

```powershell
$env:PATH += ";$env:LOCALAPPDATA\idapps\bin"
idapps.exe update
idapps.exe psql --version
```

The Windows assets are standalone portable executables; WinGet distribution is deferred. An app link such as `psql.exe` selects the app when it
is not shadowed by another alias, function or executable. Use `idapps psql`,
`idapps run psql` or the full launcher/link path to select IDApps explicitly.
The commands after the app name are forwarded as received by the launcher.
PowerShell may transform native arguments before the process starts. Windows
PowerShell 5.1 can also transform binary output in pipelines and redirection;
for binary exports use the tool's file option on a mounted directory, such as
`idapps pg_dump -f /postgres/export.dump ...`.

Replace the binary in place when upgrading. If it moves, run the new binary by
full path with `update` to reconcile its owned links. Updating downloads a commit-pinned repository ZIP, validates and caches its catalogue, and changes owned links; it does not replace the binary,
restart services or delete data. Removing the disposable catalogue cache requires
another `idapps update` before starting apps. Configuration records must remain
in place for ownership and recovery; retired services can be managed by their
explicit launcher name. Existing unowned containers and volumes are not adopted
or migrated, and name conflicts must be resolved by the user.

Users of an old checkout should replace its root and `powershell` PATH entries
with the native app-bin entry, keeping the order of unrelated entries. The launcher does not edit profiles or delete old installations. On
macOS, Developer ID signing and notarization are deferred. Current candidate artifacts are not Developer ID signed or notarized, and Gatekeeper may prevent them from opening. Follow the operating system's first-launch guidance; do not disable Gatekeeper. Linux releases are built
without libc dependencies; actual OS, Docker, terminal, image and platform
compatibility must be verified on the target system before release.

## Release maintenance

The manually dispatched `build release assets` workflow builds the five supported targets and generates SHA-256 checksums for the final binaries. It uploads the candidate assets, checksum file, license and README as a workflow artifact. Apple Developer ID signing and notarization are deferred; the workflow does not require Apple credentials.

Before publishing, complete the native platform, image and release checks listed in the [verification record](docs/verification.md). Run the workflow on the existing `vMAJOR.MINOR.PATCH` tag, provide the same version without `v`, and explicitly enable its `publish` input. The publication job verifies the tag, commit and checksums and creates a GitHub Release; it does not publish a WinGet package. A run with `publish` disabled only produces workflow artifacts.

The catalogue uses `alpine/kubectl:1.37.2`. Kubernetes supports kubectl within one minor version of the API server; verify your cluster version before use.

## Development testing

Go tests use Ginkgo v2 and Gomega. Run the suites with `go test ./...`; use `go test -v ./... -args '-ginkgo.v'` for individual behavior descriptions. Tests requiring a local Docker engine are opt-in; see [the verification record](docs/verification.md).

Docker API tests use GoMock with the Docker client's HTTP transport. Regenerate
the transport mock with `go generate ./internal/engine` after changing its interface.

`idapps version` reports the release version or, for development builds, the
last release inferred from Go build metadata plus the Git commit and an optional
`dirty` marker (for example `1.2.3+abc123.dirty`). Before the first release the
baseline is `0.0.0`. Catalogue minimum versions use Semantic Versioning 2.0.0;
build metadata does not affect precedence. Thus a pre-release build is older
than the corresponding final release. The current catalogue requires `0.0.0`,
so builds before the first release tag can activate it.

Plain `go build` derives version information from Go's embedded metadata; it
does not need Git at runtime. This inference is best effort: shallow checkouts,
missing VCS metadata, and major-version tags incompatible with the Go module
path can prevent Go from identifying the last release correctly. Official
release builds embed the verified tag version.

### Maintaining the catalogue

Each application is defined in `catalogue/<name>/app.json` and follows `catalogue/catalogue.schema.json`; shared catalogue metadata is in `catalogue/metadata.json`. Run `go run ./tools/catalogue-compat` to validate descriptors and cross-app resource conflicts; pull-request CI runs the same check. Renovate tracks image versions in the individual descriptors.
