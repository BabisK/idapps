---
title: 'Handle cancellation while starting a command container'
type: 'bugfix'
created: '2026-11-12'
status: 'done'
route: 'oneshot'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Cancellation while Docker processes a command container start can bypass SIGINT, intermittently failing the cancellation test.

**Approach:** Attempt to interrupt the owned container on cancellation during start, preserve forced cleanup and cancellation status, and add deterministic offline coverage for that timing.

</frozen-after-approval>

## Implementation Notes

- Reproduced the original failure on the ninth repeated run of the existing test.
- Attempt SIGINT with an independent timeout if container start returns after cancellation, and preserve forced cleanup. Added scenarios for cancellation during a pending start response and after the start response has been processed.
- The full offline Go suite and race-enabled engine suite passed; both cancellation scenarios passed 100 consecutive runs.

## Review Triage Log

- medium: The fake's dial context shadowed the cancellation context, potentially leaking a goroutine; fixed by using the outer context.
- medium: The post-start case did not guarantee that the client had processed the response; fixed by waiting for the input attachment to close, which occurs after start returns.
- maybe-false: A real engine could reject SIGINT if it has not completed starting the container; forced removal remains the fallback. Proving the precise engine interleaving requires a controlled real-engine test.
- false: The untracked `idapps` binary predates the work and will not be staged or removed.
