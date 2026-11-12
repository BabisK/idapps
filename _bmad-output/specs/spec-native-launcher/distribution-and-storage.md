# Release distribution and local storage

This companion defines the approved packaging and storage contract for CAP-1, CAP-3 and CAP-6.

## Release assets

Publish platform-specific assets on GitHub Releases using immutable, versioned URLs. Publish SHA-256 checksums of the final downloadable assets, plus license and installation instructions. Ship no active catalogue or app links; first `idapps update` obtains repository-root `catalogue.json` from `main`.

| Target | Downloadable format |
| --- | --- |
| Windows amd64 / ARM64 preview | Standalone portable `.exe`, installed as `idapps.exe`. |
| Linux amd64 / ARM64 | Plain executable, installed as `idapps`; no required archive or installer. |
| macOS ARM64 | Plain executable, installed as `idapps`; no required archive or installer. Developer ID signing and notarization are deferred. |

WinGet distribution is deferred, including manifest creation, validation, installation testing and publication. The release still provides architecture, version and final SHA-256 metadata for the standalone executables. IDApps keeps mutable cache, ownership state and generated app links independent of the executable's location.

Developer ID signing and notarization are deferred. Document macOS first-run Gatekeeper behavior for unsigned downloads and do not disable Gatekeeper or alter OS security settings automatically.

For manual setup, users download the correct asset, verify its checksum, place it in the app-bin directory below and set executable permission on Linux/macOS. They append that directory to PATH, preserving earlier commands. Replace only the launcher binary for a binary upgrade; catalogue updates never download or replace it. Replacement must preserve cached catalogues, resource records and existing owned links.

## Per-user locations

Use platform path APIs. Paths in this table are defaults, not literal shell expressions to evaluate. Linux honors absolute `XDG_CACHE_HOME`, `XDG_CONFIG_HOME` and `XDG_DATA_HOME`; empty/unset values use the defaults below. A relative override or unavailable base directory is an actionable error, not a reason to store data in the working directory.

| Purpose | Linux | macOS | Windows |
| --- | --- | --- | --- |
| Catalogue cache | `$XDG_CACHE_HOME/idapps/catalogues`, default `~/.cache/idapps/catalogues` | `~/Library/Caches/idapps/catalogues` | `%LOCALAPPDATA%\idapps\cache\catalogues` |
| Configuration and durable records | `$XDG_CONFIG_HOME/idapps`, default `~/.config/idapps` | `~/Library/Application Support/idapps` | `%APPDATA%\idapps` |
| App-bin directory | `$XDG_DATA_HOME/idapps/bin`, default `~/.local/share/idapps/bin` | `~/Library/Application Support/idapps/bin` | `%LOCALAPPDATA%\idapps\bin` |

Go's [`os.UserCacheDir`](https://pkg.go.dev/os#UserCacheDir) supplies the platform cache base; [`os.UserConfigDir`](https://pkg.go.dev/os#UserConfigDir) supplies the configuration base for ownership and recovery records. The [XDG base-directory specification](https://specifications.freedesktop.org/basedir/latest/) distinguishes cache, persistent state and data locations. Windows cache stays under non-roaming LocalAppData; it is not a temporary-download folder.

The catalogue cache holds downloaded JSON, staging files and immutable catalogue generations. Durable records in the configuration directory hold the installation UUID, active generation's commit/hash reference, link ownership manifest, resource creation snapshots, locks and update journal. The configuration directory does not keep a second copy of the downloaded catalogue. Saved service definitions describe actual managed resources and remain available independently of the catalogue.

Ownership and recovery records belong to their host and installation, not merely to a portable file path. Copying or roaming the configuration directory does not transfer ownership: reject another host's record/journal before changing links or Docker resources, and retain engine-identity checks.

The initial layout provides one managed installation per OS user. Invocation paths share its ownership identity and locks. State is independent of the executable's parent directory, working directory and release version. Manual setup normally colocates the binary and app links in app-bin. If the binary is installed elsewhere, record its target explicitly; create relative app links when colocated and absolute links otherwise. Every link still targets the single launcher executable.

Replacing that binary at the same path keeps links valid. After moving it or a package-manager upgrade that changes its path, invoke the new launcher explicitly with `idapps update` to reconcile only previously owned links to its new location. Ordinary app calls never silently change the registered target. IDApps does not copy binaries or mutate package-manager installation files. In-place replacement, including through a stable package-manager entry point, is preferred.

Restrict managed writes to the resolved IDApps cache, state and app-bin roots; the executable may be outside those writable roots. Reject unexpected symlinks/reparse points within managed write paths, and retain all foreign-file and foreign-link protections. Missing permissions or unresolved user directories fail clearly without automatic elevation or fallback writes beside the binary.

## Clearing the cache and recovering updates

Clearing downloaded catalogues must not delete app links, discard ownership, relabel Docker resources or remove containers/volumes. Without a valid active catalogue, a new command or service start fails with `idapps update` guidance. No app invocation silently downloads a replacement catalogue.

`version` can show the last active commit and missing-cache status; `list` reports that definitions are unavailable while still exposing recorded managed services. Explicit `idapps NAME stop`, `logs` and applicable `clean` (or `idapps run NAME …`) can use saved owned-service records even when the catalogue cache is absent. Normal ownership, engine-identity and cleanup checks still apply. A subsequent successful update re-downloads and validates the catalogue, then reconciles links using durable ownership records.

The update journal and active-generation reference must survive cache deletion. A transaction stages and atomically renames files within their own destination filesystems, then uses the durable journal to coordinate cache, state and link changes; do not assume these directories share a filesystem or that one cross-directory rename makes the whole update atomic.

If a cache cleaner removes a generation during an update or recovery, restore consistent ownership/link metadata where possible and report the missing catalogue. Never execute a partially available generation, infer ownership from a cache file, or promise offline execution after its required bytes have been deleted. Recovery can require another `idapps update` to restore missing catalogue bytes; it must retain the durable records needed to avoid touching unrelated files and resources.
