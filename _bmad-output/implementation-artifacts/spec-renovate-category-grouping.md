---
title: 'Group Renovate updates by dependency category'
type: 'chore'
created: '2026-11-12'
status: 'done'
route: 'oneshot'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Dependency updates currently create separate pull requests rather than one per requested category.

**Approach:** Group GitHub Actions updates, Go module updates, and catalogue image updates into three separate Renovate pull request groups. Include major and non-major updates in each category, and preserve the existing catalogue discovery, schedules, and dashboard policy.

</frozen-after-approval>

## Implementation Notes

- Added manager-specific package rules with stable group slugs and disabled major/minor separation for each requested category. The catalogue rule is limited to `catalogue.json`.
- Renovate 44.226.2 strict validation passed. A local harness using Renovate's package-rule and branch-name logic checked major, minor, patch, pin, pin-digest and digest updates for all three categories. Security remediation and lockfile-maintenance branches retain Renovate's special handling.

## Review Triage Log

- false: The untracked `idapps` binary predates this work and is excluded from the commit; the user authorized leaving it untouched.
- false: A local harness exercised Renovate 44.226.2 package-rule matching and branch generation for each manager and major, minor, patch, pin, pin-digest and digest updates; it does not simulate the hosted service.
- false: Implementation notes and final status were populated after review.
