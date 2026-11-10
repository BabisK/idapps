---
title: 'PowerShell command compatibility'
type: 'feature'
created: '2026-11-10'
status: 'in-review'
baseline_commit: 'b377589f72670be7acd329d5ad6a0ad04e2fba8e'
route: 'dispatch'
review_loop_iteration: 0
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The Docker-backed commands currently require a POSIX shell, so Windows PowerShell users cannot invoke them naturally.

**Approach:** Provide Windows PowerShell 5 and newer entry points for every existing wrapper, retaining names, service behavior and existing command precedence. The complete approved requirements are in `_bmad-output/specs/spec-powershell-commands/SPEC.md`.

## Boundaries & Constraints

**Always:** Support actively supported Windows versions where prerequisites are available. Preserve POSIX behavior and local-development scope. Forward argument values and order, interactive and redirected streams, exit status, current-directory mounts and applicable credentials. Existing aliases, functions and native executables win; full-path script invocation selects IDApps. Report missing Docker CLI and unreachable daemon clearly with nonzero status. Keep image versions synchronized with the existing shell wrappers. Test PowerShell 5.1 and a supported PowerShell 7 on Windows; report untested configurations.

**Never:** Replace existing commands or require unsupported Windows environments to work. Do not expose credentials in test output or permit cleaning a running service's volume.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| CLI | `psql` without conflict; arguments including spaces, quotes, empty strings | Starts corresponding Docker client; preserves arguments, streams and status | Propagate failure status |
| Conflict | Existing alias, function or executable shares name | Existing command wins; full script path selects IDApps | Document resolution |
| Service | No arguments, `start`, `stop`, `logs`, `clean` | Preserve existing behavior and options | Reject clean while running without removing volume |
| Docker | CLI missing or daemon unreachable | No container action | Explain cause and return nonzero |
| Paths | Working or credential paths contain spaces or Unicode | Correct paths mounted and available | Do not reveal credentials |

</frozen-after-approval>

## Code Map

- Root wrapper files define 22 `IMAGE_NAME`/`IMAGE_VERSION` pairs; clients and services sometimes share versions. Read them as the source of image pins so Renovate updates remain effective.
- `lib/clean.sh`, `lib/logs.sh`, `lib/stop.sh` define running-volume guard, `logs -f/-t`, and stop behavior. Root service wrappers define names, ports, volumes and environment.
- `aws`, `helm`, `helmfile`, `kubectl` mount host configuration; other CLI wrappers define their own workdirs, entrypoints and arguments.
- `README.md` describes setup, local-only scope and service lifecycle. Preserve these while adding PowerShell instructions.
- `_bmad-output/specs/spec-powershell-commands/SPEC.md` is the approved feature contract and stays with this implementation.

## Tasks & Acceptance

**Execution:**
- [x] `powershell/` — implement one explicit-path entry point per root wrapper and shared dispatch with Windows-compatible Docker invocation; keep pin definitions sourced from existing scripts.
- [x] `powershell/` — preserve the individual service and client options, mounts, working directories, lifecycle and logging; provide safe command resolution setup.
- [x] `tests/` — add meaningful automated checks for command coverage, Docker calls, argument boundaries, collisions, Docker failures and lifecycle safety; exercise the matrix on available Windows runners.
- [x] `README.md` — document PowerShell installation, precedence and explicit paths, prerequisites, networking and limitations.
- [x] `_bmad-output/specs/spec-powershell-commands/` — retain the approved spec and its decision log alongside implementation.

**Acceptance Criteria:**
- Given setup on supported Windows, when each existing command is invoked, then it resolves to its Docker-backed equivalent unless a pre-existing command wins; direct script paths remain usable.
- Given a running IDApps service on a supported Docker configuration, when its client uses documented local settings, then the client connects.
- Given PowerShell 5.1 and a supported PowerShell 7 release, when validation runs, then versions and outcomes are recorded and untested supported configurations are disclosed.
- Given existing POSIX usage, when the change is applied, then its behavior is unchanged.

## Implementation Notes

- Windows PowerShell 5.1.26100.9818 and PowerShell 7.6.9 mock suites pass on Windows build 26300. Real Docker 29.9.3 Redis connectivity and interactive `PING`/`PONG` passed on PowerShell 5.1; test resources were cleaned up.

## Spec Change Log

## Review Triage Log

- `medium` Blind/edge: output-only pipelines could allocate a TTY. Invocation pipeline position is now checked; PowerShell `> file` in an interactive host remains unresolved because it reports a one-command pipeline and console output as unredirected.
- `medium` Blind: Windows PowerShell 5.1 used ASCII native pipeline encoding; UTF-8 encoding is now applied temporarily and tested.
- `medium` Blind: PowerShell pipelines are text-oriented and do not preserve arbitrary binary data; this remains a limitation against stream-preservation requirements.
- `false` Blind: substring container matching is inherited from the POSIX helpers, not introduced by this change.
- `medium` Blind: Docker stop/clean errors were suppressed; contextual failure messages are now provided.
- `medium` Blind: real mounts with spaces and Unicode were not exercised; mock assertions now cover the paths, but actual credential mounts remain unverified.
- `medium` Blind: Unicode and output-pipeline tests were missing; focused tests added, while binary and interactive file redirection remain unresolved.
- `medium` Blind: README claimed kubectl mounted the current directory; corrected to describe its actual behavior.
- `medium` Verification gap: service startup arguments were not checked; tests now verify service options and image.
- `medium` Verification gap: client image and executable selection were not checked; tests now cover both.

## Design Notes

Add a dedicated script directory at the end of PATH so pre-existing executable paths retain precedence; verify PowerShell discovery and execution policy on Windows. PowerShell 5.1 native argument quoting requires special attention, as does noninteractive stdin. Docker Desktop host networking is opt-in; do not assume Linux host behavior. Windows may not be available in the implementation environment; distinguish automated mock checks from actual Windows integration evidence.

## Verification

Run automated PowerShell tests on Windows PowerShell 5.1 and PowerShell 7 and check representative real Docker client/service connectivity. Verify shell wrapper behavior is unchanged. Record any environment-limited checks explicitly.
