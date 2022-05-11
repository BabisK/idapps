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

In the same spirit, commands mount the current path in their workspace, use the host user and group id to avoid
permissions issues and forward all user parameters to the container. So, running a command with IDApps should be
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