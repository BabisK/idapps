---
title: 'Download catalogue as a repository archive'
type: 'feature'
created: '2026-11-13'
status: 'done'
route: 'dispatch'
review_loop_iteration: 0
baseline_commit: 'b49389d080634c58adc4972012eed59b24adb1e1'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Cross-app conflicts should be caught in repository checks before merging, and the launcher currently fetches individual files and serializes them into a combined catalogue.

**Approach:** Verify catalogue conflicts in pull-request CI. Download a complete GitHub repository ZIP for each update, extract the catalogue from it, and load individual descriptors without creating a combined catalogue document for new generations.

## Boundaries & Constraints

**Always:** Resolve main to an immutable commit and fetch its archive; preserve schema, semantic, version, and resource-ownership validation, crash-safe link reconciliation, and existing ZIP caches. Fail safely on incomplete, malformed, oversized or unsafe archives. Preserve user-owned files, containers and volumes. CI must reject cross-app container-name and volume-name conflicts before merge. Use the contributor-facing descriptor schema as the sole app schema.

**Never:** Require Git, a shell or Docker CLI for users; modify Docker resources during update; extract unrelated archive contents onto the filesystem; require contributors to maintain a combined JSON catalogue.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Update | Complete ZIP at resolved commit | Load catalogue descriptors, cache a complete generation and reconcile owned links | Existing resources remain untouched |
| Bad archive | Truncated, oversized, unsafe paths, duplicate entries or invalid descriptors | Reject generation | Keep active cache, links and resources unchanged |
| Pull request | Two apps use the same container or named volume | Catalogue CI validation fails | Report a useful conflict error |

</frozen-after-approval>

## Code Map

- `internal/update/fetch.go` — resolves `main`, fetches tree and raw files; replace latter with one bounded ZIP request pinned to SHA, with trusted HTTPS origin and redirect restrictions.
- `internal/catalogue/tree.go`, `catalogue.go`, `catalogue/schema_embed.go` — `Assemble` and `Parse` currently produce and consume aggregate JSON. Refactor reusable descriptor/tree validation to build `Catalogue` directly; Validate embedded contributor schema and existing semantic checks.
- `internal/update/state.go` — `Generation.Bytes`, hash-addressed `.json` caches, journals and `Snapshot`; preserve atomic recovery while caching archives as immutable generations and revalidating them on load.
- `internal/update/fetch_test.go`, `state_test.go`, `cmd/root_test.go`, `internal/catalogue/catalogue_test.go` — existing Ginkgo scenarios for pinning, validation, rollback and cache integrity; adapt without deleting meaningful coverage.
- `tools/catalogue-compat/main.go`, `internal/compat/`, `.github/workflows/go.yml` — CI already invokes compatibility validation on PRs; make conflict checking explicit and test independently.
- `README.md`, `docs/verification.md` — adjust update, contributor and verification documentation where relevant.

## Tasks & Acceptance

**Execution:**
- [x] `internal/catalogue/` — validate metadata and individual descriptors from a complete tree, building an in-memory collection without producing combined JSON; retain legacy parsing for old caches and compatibility tooling.
- [x] `internal/update/fetch.go` — download a bounded ZIP pinned to the resolved commit, safely select the complete catalogue subtree and reject malformed or ambiguous archives; add meaningful archive, pinning and failure tests.
- [x] `internal/update/state.go` — cache and verify complete new archive generations atomically, support old `.json` generations and journals, and maintain link and resource safeguards; test corruption and rollback.
- [x] `tools/catalogue-compat/`, `.github/workflows/go.yml` — ensure PR checks report and reject cross-app conflicts; test both container and volume collisions.
- [x] `cmd/root_test.go`, `README.md`, `docs/verification.md` — update end-to-end mocks and documentation for ZIP downloads and generation handling.

**Acceptance Criteria:**
- Given a valid repository ZIP, when an update runs, then all existing apps are available with unchanged definitions and no combined catalogue is written for the new generation.
- Given an app conflict, when repository validation runs in a PR, then the check fails before merge.

## Implementation Notes

Implemented commit-pinned ZIP download, direct tree parsing and archive-backed cache. Full Go suite, compatibility check and isolated real GitHub archive smoke test passed.

## Spec Change Log

- User clarified there are no legacy installations; removed legacy JSON parsing and aggregate assembly, and switched repository checks and fixtures to descriptors and ZIP archives.

## Review Triage Log

- medium: Metadata fractional schema versions could round to one; fixed typed decoding and added a regression test.
- medium: Directory ZIP entries could contain unchecked data; fixed and tested; the edge review reported the same issue independently.
- medium: Repeated snapshots decompressed unrelated repository files; snapshots now validate the catalogue without repeatedly expanding unrelated entries.
- medium: Existing cached generation reads were unbounded; bounded regular-file reads are now shared by activation and snapshot.
- medium: HTTP, invalid commit and oversized input regressions lacked coverage after the downloader replacement; restored tests.
- medium: Archive type, path collision, expanded-size and checksum failures lacked coverage; added cases.
- medium: ZIP transaction-boundary rollback was untested; added recovery tests for ZIP generations.
- medium: Compressed and expanded ZIP limits lacked tests; added both cases.
- medium: Unsafe ZIP entry types lacked tests; added a symlink case.
- medium: Production redirect policy lacked tests; added trusted and untrusted redirect cases.

## Design Notes

ZIP bytes may serve as the immutable cache generation, avoiding filesystem extraction and a new combined representation. Select only the catalogue subtree in memory, strictly validate paths, entry types, duplicates, sizes and completeness, and bound compressed and decompressed input. Use a commit-addressed archive URL rather than a moving branch archive so the saved revision identifies what was fetched.

## Verification

**Commands:**
- `go test ./...` — all applicable tests pass.
- `go run ./tools/catalogue-compat` — repository catalogue and supported release parsers pass.
