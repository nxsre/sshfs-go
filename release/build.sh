#!/usr/bin/env bash

if [ ! -d .git ]; then
    echo "this script should be run from the root of the project"
    exit 1
fi

if ! docker images | grep -q sshfs-build; then
    docker build -t sshfs-build release/
fi

docker run --rm -v $(pwd):/go/src/github.com/soopsio/sshfs-go sshfs-build /bin/sh -c 'cd /go/src/github.com/soopsio/sshfs-go && env GOOS=linux GOARCH=amd64 go build .'
