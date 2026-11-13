# Instant Docker Apps

IDApps is a Go launcher for Docker-backed developer tools and short-lived services. Application definitions live in `catalogue/<name>/app.json` and must satisfy `catalogue/catalogue.schema.json` and the semantic checks in `internal/catalogue`. Usage and lifecycle behavior are documented in `README.md`.

## Policy

- Use Conventional Commits for commit messages.
- Use behavior-driven development for Go with Ginkgo v2 and Gomega. Group related observable behaviors with `Describe` and `When`/`Context`, share scenario setup where useful, and report meaningful cases independently. Avoid mechanically wrapping traditional tests in Ginkgo blocks.
- Keep Go tests alongside the code they exercise, in the corresponding `*_test.go` file. Separate files are appropriate for generated mocks, suite bootstraps and platform-specific build constraints.
- Keep these applications for local development; their default credentials and settings are not intended for production.
- The launcher communicates with a local Docker Engine API directly and does not require the Docker CLI or a shell runtime.
- Preserve user-owned files, containers and volumes. Updates must not change Docker resources.

## Where things are

- `main.go` is the executable entry point; `cmd/` contains the Cobra root and command files.
- `internal/catalogue/`, `internal/storage/`, `internal/update/` and `internal/engine/` contain validation, per-user state, reconciliation and runtime behavior.
- `_bmad-output/specs/spec-native-launcher/` contains the approved launcher contract and companions.
- `docs/verification.md` records tested environments and verification gaps.

Application descriptors live in `catalogue/<name>/app.json`, with shared metadata in `catalogue/metadata.json`. Validate the catalogue with `go run ./tools/catalogue-compat`.
