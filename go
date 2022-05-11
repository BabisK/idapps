#!/bin/sh

set -e

IMAGE_NAME=golang
IMAGE_VERSION=1.18.2-bullseye

docker run --rm -it --user "$(id -u)":"$(id -g)" --net=host -v "$(pwd)":/src -w /src $IMAGE_NAME:$IMAGE_VERSION go "$@"