---
title: 'Remove unused catalogue code'
type: 'refactor'
created: '2026-11-13'
status: 'done'
route: 'oneshot'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The repository contains data and branches no longer used after the catalogue update changes.

**Approach:** Remove proven unused functionality while preserving validation, catalogue behavior and platform-specific code.

</frozen-after-approval>

## Implementation Notes

- `internal/update/fetch.go`: `Generation.Catalogue` is only read by a test; retain archive validation and remove the unused result. The expanded-size check after the overflow-safe pre-addition guard is unreachable.
- `internal/catalogue/catalogue.go`: `Catalogue.SchemaVersion` and `Catalogue.MinimumLauncherVersion` have no consumers; validate the minimum directly without retaining metadata in the result.
- `internal/update/fetch_test.go`: assert the fetched archive is valid without relying on an unused result field. Run the catalogue and update tests and the full suite if feasible.
- The workflow files, Renovate manager and BMAD artifacts remain referenced or document historical contracts; do not remove them.

## Review Triage Log

- low: Downloaded bytes were not asserted exactly; added comparison with the response body.
- low: The expanded-size guard lacked a cumulative multi-entry scenario; added a bounded archive-header test.
- low: The returned generation hash was not checked against the downloaded bytes; added a digest assertion.
