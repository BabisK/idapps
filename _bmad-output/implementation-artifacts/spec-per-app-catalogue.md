---
title: 'Independent app catalogue descriptors'
type: 'feature'
created: '2026-11-12'
status: 'done'
route: 'dispatch'
review_loop_iteration: 0
baseline_commit: '36ccebcc4eb8a3453e125c441874d963bbfc208f'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A single catalogue file makes app definitions harder to maintain independently and increases contribution conflicts.

**Approach:** Store each app in its own directory under a catalogue tree, download that tree on update, and assemble and validate the same catalogue state as today.

## Boundaries & Constraints

**Always:** Preserve all existing application definitions and behavior, pinned-commit fetching, bounded untrusted input, complete validation before activation, atomic reconciliation, existing cache and service snapshots. Preserve compatibility for released launchers using the existing root catalogue through a generated aggregate and check its freshness in CI. Document how contributors regenerate it. Keep Renovate updating descriptor images.

**Never:** Change Docker resources during update, overwrite user-owned resources, or require Docker CLI, a shell runtime, or Git on end-user systems.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Update | Pinned tree with valid descriptors | Assemble, validate and activate equivalent aggregate | Preserve existing reconciliation rules |
| Invalid tree | Missing, invalid, duplicate, oversized or conflicting descriptors; incomplete download | Reject the entire generation | Retain active state and owned resources |
| Compatibility | Existing cached aggregate or older launcher | Existing cache remains readable; generated root aggregate remains available | Reject stale generated aggregate in CI |

</frozen-after-approval>

## Code Map

- `catalogue.json`, `catalogue.schema.json`, `internal/catalogue/schema.json` — existing aggregate and schema; retain aggregate as generated compatibility artifact, preserve validation and schema parity.
- `internal/catalogue/catalogue.go` — `Parse`, `CheckJSON` and semantic cross-app checks; assemble raw JSON safely before parsing so duplicate keys cannot disappear.
- `internal/update/fetch.go` — resolves main to SHA, limits downloads, validates and hashes bytes. Fetch the tree at the pinned commit; retain trusted-origin and all-or-nothing guarantees.
- `internal/update/state.go`, `cmd/update.go` — aggregate cache, transactions and resource identity protections; reuse without changing Docker resources.
- `internal/compat/compat.go`, `tools/catalogue-compat/main.go`, `.github/workflows/go.yml` — release-parser compatibility and CI; check generated aggregate matches tree.
- `renovate.json` — JSONata image manager currently targets aggregate; switch to descriptor paths and image shape.
- `README.md`, `AGENTS.md` — update contributor and catalogue instructions. Existing tests read root aggregate as fixture; keep those tests useful.

## Tasks & Acceptance

**Execution:**
- [x] `catalogue/`, `catalogue.json` — split every existing app into its own directory with a JSON descriptor; retain catalogue metadata and reproducibly generate the root compatibility aggregate.
- [x] `internal/catalogue/`, `tools/` — implement shared tree assembly and deterministic generation, checking malformed JSON, names, limits and cross-app validation; add behavior tests including parity with the current catalogue.
- [x] `internal/update/fetch.go`, `internal/update/fetch_test.go`, `cmd/root_test.go` — fetch complete pinned tree and assemble it; test errors, pinning, and transactional preservation.
- [x] `renovate.json`, `.github/workflows/go.yml`, `README.md`, `AGENTS.md` — adapt automation and contributor guidance, including compatibility generation.

**Acceptance Criteria:**
- Given the current catalogue, when generated from app directories, then the same app definitions and metadata pass existing semantic and release compatibility checks.
- Given an app contribution, when its descriptor is added and the aggregate regenerated, then validation and image automation recognize it independently.
- Given an active generation, when update encounters an invalid or incomplete tree, then the active generation and user resources remain unchanged.

## Implementation Notes

Implemented per-app descriptors, deterministic compatibility generation, pinned subtree downloads and Renovate regeneration. Full Go suite and compatibility check passed.

## Spec Change Log

## Review Triage Log

- medium: Renovate updates left the aggregate stale; fixed with an allowlisted generation task.
- medium: Recursive repository-wide tree listing could exceed its limit due to unrelated content; fixed by fetching the catalogue subtree.
- low: Sequential per-file requests may take longer on slow networks; existing client timeouts bound each request and catalogue remains small.
- low: Metadata referenced an incompatible nonexistent schema; removed the reference and retained the generated aggregate schema.
- medium: Fetch failure and pinning tests had been removed; restored coverage.
- medium: Missing `truncated` was accepted; now both tree responses require explicit false.
- low: Root symlink handling was confusing; a symlink root fails assembly rather than accepting outside content.
- medium: Filesystem symlink rejection lacked coverage; added a test.
- medium: Renovate and missing completeness flag findings from edge review duplicate the independently fixed issues above.

## Design Notes

Use a deterministic aggregate of raw descriptors so the existing parser and cached format remain authoritative. Resolve main once and enumerate and fetch only catalogue files at that commit, with bounded responses and strict path/type checks. Keep the root aggregate generated for already released launchers that still request it directly.

## Verification

**Commands:**
- `go test ./...` — all applicable tests pass.
- `go run ./tools/catalogue-compat` — generated aggregate is fresh and compatible with supported releases.
