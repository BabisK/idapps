# Instant Docker Apps

## Description

Instant Docker Apps is a collection of developer oriented, opinionated, portable, self-cleaning scripts to execute
common applications (databases, CLIs, tools, management consoles etc).

### Developer oriented

The target user group are definitely developers and devops engineers. While filling these two roles, I frequently need
to run a database to test an app, a cli to configure a cloud etc. and I dislike installing single use stuff on my
laptop. So, for several years I've been using Docker, but I constantly find myself reading the same docs in DockerHub as
I don't remember parameters.

IDApps tries to resolve this constant documentation lookup by scripting the launch of these applications.

**WARNING**: Do not use any of these in production! They are not meant to! There are default values and passwords
everywhere. The goal of the project is to simplify deploying local, short-lived, development environments.

### Opinionated

These scripts have minimal configuration parameters, if any, as the goal is not to cover all use cases but to get the
apps up and running as soon as possible.

The application in this collection can be categorized in *services* and *commands*. Services are long-running
systems such as databases. Commands are single shot applications like `psql` or `aws-cli`.

In general services use some default user (no security), occupy a host port - either by port-mapping or by using host
networking (so, you cannot run multiple instances of the service) and use docker volumes for persistence.

In the same spirit, most commands mount the current path in their workspace; `kubectl` mounts only its configuration.
The POSIX wrappers use the host user and group id to avoid permissions issues and forward all user parameters to the container. So, running a command with IDApps should be
identical to running it if it was installed.

### Portable

The scripts have only two dependencies; docker and a POSIX shell. So, they should run everywhere: Linux servers, WSL2 in
laptops, MacOS, a coffee machine and whatnot.

### Self-cleaning

Don't you hate it when your disk fills up with stopped containers? I do too. All scripts try their best to clean up
after themselves.

## Installation

1. Clone/download-and-extract this repository somewhere in your system.
2. Add the root folder of the repository in your path (e.g. export PATH=$PATH:$PWD)
3. Ready!

## Common interface

Most services have 3 execution targets:

1. `start`: This should start the service. This is the default target, so if not target is provided, this will be 
executed
2. `stop`: This should stop the service and remove the container. The persistent storage remains intact, so the next
time you start it, it will pick it up and use it.
3. `clean`: This should remove the persistence storage. If you messed up and need to start over, this is the target to
use.
## Windows PowerShell

The `powershell` directory provides Windows PowerShell 5.1 and PowerShell 7
entry points. Install Docker Desktop with Linux containers enabled, start its
engine, and clone or extract the entire repository. In PowerShell, append the
entry-point directory to your PATH (replace the path with your checkout):

```powershell
$env:PATH += ';C:\path\to\idapps\powershell'
psql --version
```

Add the PATH line to your PowerShell profile to keep it for future sessions.
Your execution policy must allow local scripts; for example, an administrator
may configure `RemoteSigned` for your account. Files downloaded from the web
may need to be unblocked. Use `Get-Command psql -All` to inspect resolution.
Aliases and functions take precedence; an executable earlier in PATH also wins.
Invoke the script by full path to select IDApps in case of a conflict:

```powershell
& 'C:\path\to\idapps\powershell\psql.ps1' --version
& 'C:\path\to\idapps\powershell\postgres.ps1' start
& 'C:\path\to\idapps\powershell\postgres.ps1' logs -f -t 50
& 'C:\path\to\idapps\powershell\postgres.ps1' stop
& 'C:\path\to\idapps\powershell\postgres.ps1' clean
```

The clients use Docker host networking as the shell wrappers do. On Docker
Desktop this requires a version supporting host networking and enabling it in
Settings > Resources > Network; restart Docker Desktop after changing it.
With host networking enabled, for example, start `postgres`, then connect with
`psql -h localhost -U postgres` (password `postgres`). Published service ports
are accessible on localhost. Host networking has Docker Desktop limitations;
consult Docker's host networking documentation for your platform.

PowerShell mounts the current filesystem directory and applicable configuration
under your Windows home directory, including `.aws` and `.kube`. Docker Desktop
must have access to those paths; credential helpers or paths referenced inside a
configuration file must also be available inside the container. Unlike POSIX
wrappers, Windows clients run as the image's default user because Windows has no
portable POSIX UID/GID. Files created in bind mounts may therefore have different
ownership. TTY allocation is omitted for redirected input or output, allowing
pipelines; terminal behavior depends on the host and container application.
Images and service credentials remain intended for local development only.

Run the mock checks in either PowerShell version with
`powershell -NoProfile -ExecutionPolicy Bypass -File .\tests\powershell-tests.ps1`
(or `pwsh` in place of `powershell`). The checks do not replace real Docker
connectivity and interactive terminal tests. Development verification used Windows
PowerShell 5.1.26100.9818 and PowerShell 7.6.9 on Windows build 26300 with a
mock Docker executable, and a Redis service and client connection on Docker
Desktop engine 29.9.3 using PowerShell 5.1, including an interactive Redis
client session. Other supported Windows and Docker versions have not been verified
in this environment.

PowerShell pipelines transmit text rather than raw bytes, so binary input is not
guaranteed to survive a pipeline unchanged.
