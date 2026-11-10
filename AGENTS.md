<!-- bmad:context -->
<!-- Verified 2026-11-10 against e4d4893534ddd472ad7eb93766112634d07c01b9. Managed by bmad-project-context; edits inside this block are replaced on refresh. Keep anything you want preserved outside the markers. -->

## Instant Docker Apps

Portable POSIX shell wrappers run developer tools and short-lived services in Docker. Usage and the service interface are documented in `README.md`.

## Policy

- Use Conventional Commits for commit messages.
- Keep these scripts for local development; their default credentials and settings are not intended for production.
- Preserve Docker and a POSIX shell as the only required dependencies.

## Where things are

- Service wrappers source shared lifecycle helpers from `lib/`; changes there can affect multiple services.
- Follow the service and command behavior described in `README.md` when changing wrappers.

<!-- /bmad:context -->
