---
id: SPEC-powershell-commands
companions: []
sources: []
---

# PowerShell command compatibility

## Why

PowerShell users need the same convenient, Docker-backed commands available to POSIX shell users without learning different command names.

## Capabilities

- **CAP-1**
  - **intent:** Invoke existing tools from PowerShell using their existing bare command names.
  - **success:** After documented setup, a bare command name starts its Docker-backed equivalent when no existing command takes precedence. For conflicts with an alias, function, or native executable, the existing command retains precedence and the IDApps script runs by full path; validate each conflict type. Validate discovery and invocation of every existing root-level IDApps command, using the full script path for conflicts. Supplied arguments, interactive input, output, and exit status reach the caller as appropriate.
- **CAP-2**
  - **intent:** Use existing service commands and their lifecycle from PowerShell.
  - **success:** Invoking a service command without arguments starts the service. A supported service can start, stop, and, where implemented, clean persistent data with the existing command and subcommand names; stopping preserves persistent data. When a service is running, `clean` fails with a clear message and leaves its persistent volume intact; after the service stops, `clean` can remove that volume. Services expose their existing `logs` operation and supported options, including `-f` and `-t`, with the same observable behavior as the POSIX wrappers.
- **CAP-3**
  - **intent:** Set up and troubleshoot PowerShell access to the commands.
  - **success:** Documentation identifies prerequisites, setup, command-name conflicts, and platform limitations and shows how to invoke the IDApps script by full path when a conflict exists. If the Docker CLI is missing, or the Docker daemon is stopped or unreachable, the invoked command exits nonzero and reports a clear message identifying the problem.

## Constraints

- Preserve existing POSIX-shell behavior and the project's local-development-only scope.
- Support PowerShell 5 and newer.
- Include all existing commands and services.
- Support Windows operating systems actively supported by Microsoft where compatible Docker and PowerShell prerequisites are available.
- Existing aliases, functions, and native executables take precedence over an IDApps command of the same name; invoking the IDApps script by full path selects it instead.
- Setup must preserve the resolution of an existing native executable with the same name, including when it is found through `PATH`. Validate the resolved command before and after setup; the IDApps version remains available by its full script path.
- Preserve argument order, boundaries, and values when forwarding arguments to the container. Validate multiple arguments, empty arguments, and arguments containing spaces or quotes.
- Preserve exit status, current-directory access, and interactive operation where applicable.
- CLI wrappers support interactive terminal use and redirected or piped input and output where the underlying tool supports them. Preserve standard output, standard error, and the underlying command's exit status. Validate both interactive and noninteractive use.
- On a supported Windows and Docker configuration, validate that an IDApps client connects to its corresponding IDApps service using the documented local connection settings. Document any required Docker Desktop networking configuration.
- Validate current-directory access and the configuration or credential mounts used by `aws`, `helm`, and `kubectl`. Include Windows paths containing spaces and Unicode characters, and avoid exposing credential contents in test output.
- Validate on Windows PowerShell 5.1 and at least one supported PowerShell 7 release on Windows. Record the Windows, PowerShell, and Docker versions used, and document any supported configurations that were not tested.
- Account for Windows Docker networking, mounts, user identity, terminal behavior, and credential access; document any unavoidable platform differences.

## Non-goals

- Production deployment or hardening of the bundled services.
- Changing the names or intended behavior of existing POSIX commands.
- Supporting Windows environments that cannot meet compatible Docker or PowerShell prerequisites.

## Success signal

On a supported PowerShell installation without a `psql` name conflict, a user follows the documented setup and runs `psql` by name to reach the Docker-backed client; representative CLI and service lifecycle checks pass without breaking the existing POSIX usage.

