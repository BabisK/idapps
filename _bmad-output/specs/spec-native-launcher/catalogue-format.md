# Catalogue format

This companion defines schema version 1 for CAP-2. [catalogue.schema.json](catalogue.schema.json) is the machine-readable structural contract. [catalogue.example.json](catalogue.example.json) is a complete initial catalogue of the 22 current apps, using repository values at `cdcc384`, except the user-approved `mysql:8.0.46` and `alpine/kubectl:1.37.2` replacements; image availability is documented in `docs/verification.md`. During implementation, publish the production catalogue and schema at repository root. Example files here are specification artifacts.

## Envelope and embedded images

| Field | Contract |
| --- | --- |
| `$schema` | Optional editor hint. The launcher uses its embedded validator and never downloads code or schemas from this value. |
| `schemaVersion` | Required integer `1`. Unsupported versions fail the whole update. |
| `minimumLauncherVersion` | Required Semantic Versioning 2.0.0 version, including optional pre-release and build metadata. Compare using SemVer precedence; build metadata does not affect precedence. An older launcher retains its current catalogue and reports the required upgrade. |
| `apps` | Required map of command names to definitions, each containing its complete `image` object. No top-level `images` map or image-key references are accepted. An empty map is valid and retires all owned app links without removing service data. |

Each app embeds `image: {repository, tag, digest?}`. `repository` and `tag` are required; optional `digest` is `sha256:` plus 64 lowercase hex digits and is authoritative when present. Related apps repeat these values intentionally, allowing automatic mechanical updates without shared references.

`repository` is a Docker image repository, optionally registry-qualified, without an embedded tag, digest, URL scheme or credentials. A registry port is permitted. Runtime selects `repository@digest` when pinned, otherwise `repository:tag`; changing only a tag beside an unchanged digest does not change the selected image. No implicit `latest` or host-platform override is added. The selected image must support the engine's Linux architecture.

The fetched Git commit and SHA-256 of the downloaded bytes identify a catalogue revision in local state; maintainers need not bump a redundant catalogue release number for each edit. Launcher version and schema version have separate meanings: a compatible text edit does not require a binary release, but a new engine feature may require a newer launcher and possibly a new schema.

## Application fields

| Field | Contract |
| --- | --- |
| Map key | Canonical command name, lowercase ASCII, matching `[a-z][a-z0-9_-]{0,62}`. `idapps`, management names (`update`, `run`, `list`, `version`, `help`) and Windows device names (`con`, `prn`, `aux`, `nul`, `com1`–`com9`, `lpt1`–`lpt9`) are reserved. No extensions, separators or path components. |
| `kind` | `command` or `service`; determines argument handling and lifecycle. |
| `image` | Required inline object `{repository, tag, digest?}`; no string reference or shared map. |
| `entrypoint` | Optional literal string array. Omitted inherits the image entrypoint; `[]` clears it. |
| `args` | Optional literal string array. For commands, an explicit array is prepended to user arguments, even if empty. If omitted, supplied user arguments replace the image CMD; with no user arguments, inherit the image CMD. Services use this array as CMD, or inherit image CMD when omitted. No shell parsing or interpolation. |
| `workdir` | Optional absolute POSIX container path. Omitted inherits image configuration. |
| `environment` | Optional string-to-string map merged over image environment. Values are literal; absent host variables are not injected. Empty string is meaningful. No secrets are fetched or expanded. |
| `network` | Required `host` or `bridge`. Existing commands use host networking; services use bridge networking and published ports. |
| `user` | Required object keyed by Go OS names: `linux` and `darwin` (macOS) are `host` or `image`; `windows` is `image`. `host` uses the invoking Linux/macOS process's UID:GID. `image` leaves Docker's user setting unset. |
| `mounts` | Optional list of structured binds or named volumes, below. Empty/omitted means no mounts. |
| `containerName` | Required for services only. Exact Docker container name; no substring lookup. |
| `ports` | Required array for services only. Each entry has `hostPort`, `containerPort` (1–65535), `protocol` (`tcp`/`udp`) and `hostIP` (`0.0.0.0`/`127.0.0.1`). Empty means no published ports. |

No additional properties are permitted at any object level. Strings passed to Docker cannot contain NUL; environment keys must match `[A-Za-z_][A-Za-z0-9_]*`. A service cannot use `host` user identity on Linux or macOS. Commands cannot declare named volumes or service-only fields. Service `host` networking requires an empty ports array.

TTY and input behavior, lifecycle verbs, auto-removal, installation labels and ownership are launcher rules rather than arbitrary catalogue hooks. Service clean support is derived from the presence of named-volume mounts.

## Mounts and paths

| Type | Fields and behavior |
| --- | --- |
| Bind | `{type: "bind", source: {base: "cwd" | "home", path: "relative/path"}, target: "/container/path", create: false, readOnly: false}`. `path` may be empty. Resolve the base on the machine running the launcher, then translate the result for its local Docker engine. |
| Volume | `{type: "volume", name: "postgres", target: "/var/lib/postgresql/data", readOnly: false}`. Service only; the engine supplies storage. Creation and deletion follow ownership rules in the runtime companion. |

`create` and `readOnly` are required booleans so intent is explicit. `create: true` creates missing host directories, never files; `false` requires a directory to exist. The current directory must exist and cannot request creation. Bind `path` uses forward-slash-separated segments with no empty interior segment, `.`, `..`, backslash, colon, rooted path or environment/tilde expansion. Source resolution supports spaces and Unicode. Container paths are absolute POSIX paths with no `.`/`..` segments. Reject duplicate mount targets after normalization and Docker socket paths, including resolved host symlinks to known engine sockets; this format offers no Docker-socket mount capability.

Existing filesystem symlinks within the user's home/current directory may resolve to normal directories outside that base: credential directories are not a sandbox. Report mount permission/sharing errors without printing credential file contents. Never fabricate Windows paths by splitting strings on colons.

## Whole-catalogue validation

Validation is more than JSON Schema. Before activation:

1. Require a bounded UTF-8 JSON document, no duplicate object keys, trailing document or unknown fields. Do not silently coerce types or skip invalid apps.
2. Validate schema, minimum launcher version, portable names (including reserved management names) and every embedded image definition. Validate repository/tag syntax with a Docker reference parser; do not use shell interpretation.
3. Validate bind path rules, normalized unique targets, null-free strings, user/kind compatibility and port/network compatibility.
4. Require unique service container names and named-volume ownership across apps, and no repeated port binding inside one service. Shared host ports across different services are valid: e.g. MySQL/MariaDB and Redis/Redis Stack are mutually exclusive defaults.
5. Reject kind, container-name or named-volume identity changes for an app that still has recorded native-managed resources; report the affected app and require explicit stop/cleanup or a separately designed migration. Image/config changes are allowed but do not mutate existing resources. Removing an app is allowed and retains a retired resource record.

Checks depending on the chosen engine, current directories or image availability run at invocation time, not during a Docker-independent update. Invalid cached catalogue data or durable ownership state fails closed; never execute an unvalidated edited file. The cache-loss behavior and OS storage locations are defined in `distribution-and-storage.md`.

## Maintainer workflow

Edit `apps.NAME.image` for image changes and the containing app entry for application changes. Automated/mechanical updates operate on each targeted occurrence, including repeated repository/tag values in related apps; no shared image map or synchronization requirement is introduced. Validate structural and semantic rules in pull requests before merge to `main`. Configure Renovate to discover `apps.*.image.repository`, `tag` and optional `digest`, retaining the existing no-dashboard policy. Keep supported released launchers able to read the catalogue, or intentionally raise `minimumLauncherVersion` with an upgrade explanation. Remove the former shell `IMAGE_NAME`/`IMAGE_VERSION` Renovate discovery when the wrappers are removed; the native catalogue becomes the application-definition source. The user approved `mysql:8.0.46` for all three MySQL apps and `alpine/kubectl:1.37.2`; other baseline image pins are unchanged until explicitly approved. This change does not force a release.
