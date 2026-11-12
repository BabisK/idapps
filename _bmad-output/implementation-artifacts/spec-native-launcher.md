---
title: 'Native Go launcher and catalogue'
type: 'feature'
created: '2026-11-10'
status: 'in-progress'
baseline_commit: 'cdcc38455dd913e481d992ddafb9d776d6957f0c'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '_bmad-output/specs/spec-native-launcher/SPEC.md'
  - '_bmad-output/specs/spec-native-launcher/catalogue-format.md'
  - '_bmad-output/specs/spec-native-launcher/runtime-and-updates.md'
  - '_bmad-output/specs/spec-native-launcher/distribution-and-storage.md'
  - '_bmad-output/specs/spec-native-launcher/app-compatibility.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Shell wrappers cannot deliver consistent native Windows/Linux/macOS invocation or catalogue-only updates.

**Approach:** Implement the approved native-launcher spec and every companion in `context`; those files, their linked schema, and their complete example catalogue are the detailed contract. Preserve all 22 application behaviors and replace the existing wrappers.

## Boundaries & Constraints

**Always:** Use Ginkgo v2 and Gomega for behavior-driven Go development and testing. Use one Go codebase, local Docker Engine API, actual app symlinks, direct `idapps NAME` dispatch, safe owned-resource lifecycle, strict catalogue validation and recoverable updates. Honour configuration/cache locations, all approved OS/architecture targets and release formats, PowerShell 5+, command precedence, and the required verification matrix. Preserve user containers and data. Catalogue-only updates never replace the binary or contact Docker.

**Never:** Run Docker CLI or catalogue host hooks, contact remote Docker engines, adopt/delete foreign resources, automatically migrate databases, publish to WinGet, or claim that PowerShell 5 preserves binary pipelines. Do not publish a release as part of local implementation.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected behavior | Failure handling |
| --- | --- | --- | --- |
| Update | Valid main-branch catalogue, empty or existing installation | Validate and reconcile owned symlinks and cached generation | Roll back or recover journal without touching foreign paths |
| Dispatch | App link, `idapps NAME`, or `idapps run NAME` | Same app, argv, streams and status; existing commands keep precedence | Unknown names and missing cache produce actionable errors |
| Docker | Missing engine, image, permissions or unsupported context | Pull missing public images; use local API | Clear error; no credential helpers or remote fallback |
| Service | Start/stop/logs/clean or removed app | Preserve resources, snapshot ownership and retired management | Refuse unowned/in-use data and unsafe identity changes |
| Streams | Interactive terminal or redirected native handles | TTY only for three terminals; preserve non-TTY bytes and exit status | Restore terminal and clean owned ephemeral container |
| Storage | Cache cleared, interrupted update, moved binary | Retain configuration records and recover/reconcile safely | Require update when catalogue bytes are missing |

</frozen-after-approval>

## Code Map

- `_bmad-output/specs/spec-native-launcher/` contains approved contract, machine-readable schema and 22-app example; carry it into the implementation commit.
- Root app wrappers, `lib/`, `powershell/` and `tests/powershell-tests.ps1` supply the baseline only; remove the app wrappers and wrapper-specific tests after porting.
- `README.md`, `AGENTS.md`, `renovate.json`, `.github/workflows/renovate.yml` describe existing usage and automation; preserve Renovate scheduling/app credentials and dashboard setting.
- The former root wrappers and PowerShell scripts have been removed; `main.go`, `cmd/` and `internal/` contain the native implementation. See `../../docs/verification.md` for tested environments and remaining gaps.

## Tasks & Acceptance

**Execution:**
- [x] `go.mod`, `go.sum`, `main.go`, `cmd/` — establish the native entry point, version, dispatch and platform builds.
- [x] `catalogue.json`, `catalogue.schema.json`, `internal/catalogue/` — publish the approved data/schema and implement strict structural, semantic and version validation.
- [x] `internal/storage/`, `internal/update/` — implement OS paths, durable identity/locks, HTTPS pinned-commit fetching, journalled reconciliation and fault recovery.
- [x] `internal/engine/` — implement local Docker API selection, command streaming, TTY handling, service ownership/lifecycle and recorded-resource recovery.
- [x] `internal/*/*_test.go`, `cmd/*_test.go` — cover catalogue, paths, updates, failures, dispatch, fake Docker API, streams, lifecycle and platform-specific behavior; the coverage is not exhaustive, as recorded below.
- [x] `.github/workflows/`, `renovate.json` — add CI/release builds, checksums and Windows executable verification; defer WinGet distribution and macOS signing/notarization; discover embedded images using JSONata.
- [x] `README.md`, `AGENTS.md`, root wrappers, `lib/`, `powershell/` — document migration and remove superseded app scripts and wrapper-specific tests.
- [x] `_bmad-output/specs/spec-native-launcher/` — retain approved specification and decision log alongside implementation.

**Acceptance Criteria:**
- Given a supported system, when a user downloads a release and runs `idapps update`, then every catalogue app has a native link and direct dispatch works without Docker CLI.
- Given invalid input, an interrupted update or foreign resources, when reconciliation or lifecycle runs, then no unrelated file, container or volume is changed and recovery is actionable.
- Given a valid app, when its command or service executes via the local Engine API, then its approved settings, arguments, streams, exit status and lifecycle match the companions.
- Given a catalogue-only change, when the user updates, then additions, removals and image changes apply without a binary rebuild or service restart.
- Given release verification, when the approved platform matrix and native macOS checks run, then results and unavailable prerequisites are recorded accurately.

## Implementation Notes

- Migrated the Go tests to Ginkgo v2 and Gomega at the user's request, then reorganized them around behaviors and contexts with independently reported cases. CI displays individual spec descriptions.
- At the user's request, Cobra's root and command files execute launcher actions directly, and Viper reads the existing platform and Docker environment settings. App arguments remain opaque and the existing configuration sources and precedence are preserved.
- Replaced the catalogue JSON Schema validator with `google/jsonschema-go`; strict JSON and semantic checks remain in place. The old and new validators agreed on representative catalogue and boundary cases.
- Catalogue compatibility now follows full SemVer 2.0.0 precedence. Untagged builds use the release inferred from Go build information plus commit and optional dirty metadata; before the first tag the baseline is `0.0.0`. The user selected plain `go build` with best-effort metadata and accepted that unavailable history or incompatible major-version tags can prevent exact inference. Verified release-tag builds embed the requested version.
- Added Ginkgo acceptance scenarios with GoMock-backed Docker HTTP requests, catalogue inventory checks, executable dispatch and precedence, update download/rollback/locking, service ownership/recovery and native binary stream fixtures. Docker attach remains tested with socket-based fakes because it upgrades the connection.

## Spec Change Log

- The user deferred the `psql` host-user lookup failure to a separate spec. Its resolution is outside this implementation record.
- Automated tests remain offline; real-engine and image checks are manual, and unavailable checks remain documented as verification gaps. WinGet distribution and Apple signing/notarization are deferred.

## Review Triage Log

## Verification

- Hosted native launcher and fake-API checks passed on Ubuntu 26.04 amd64/arm64, Windows amd64 and macOS arm64 in [run 43247514060](https://github.com/BabisK/idapps/actions/runs/43247514060). The offline Go suite, race-enabled engine suite and vet have also passed locally; real Docker Redis lifecycle checks passed on Windows and Linux/WSL2 with Docker Desktop. See `../../docs/verification.md` for dates, versions and limits.
- **Remaining gaps:** Real Docker on native Ubuntu amd64/arm64 and Apple-silicon macOS; Windows ARM64 preview; execution of all catalogue images on both Linux-container architectures; complete native terminal, stream, mount and recovery coverage; and downloaded release-asset verification. Manual checks remain outstanding when suitable environments and images become available.
- **Known deferred items:** PostgreSQL host-user lookup will be handled in a separate spec; Renovate's JSONata digest-first combined update limitation is accepted for now; WinGet distribution and Apple Developer signing/notarization are deferred. Real Docker behavior when SIGINT races with an in-flight start is recorded in `deferred-work.md`.
