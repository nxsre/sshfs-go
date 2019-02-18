# SSHFS

[![Build Status](https://travis-ci.org/soopsio/sshfs.svg?branch=master)](https://travis-ci.org/soopsio/sshfs)

SSHFS mounts arbitrary [sftp](https://github.com/pkg/sftp) prefixes in a FUSE
filesystem. It also provides a Docker volume plugin to the do the same for your
containers.

<!-- markdown-toc start - Don't edit this section. Run M-x markdown-toc-generate-toc again -->
**Table of Contents**

- [SSHFS](#sshfs)
- [Mounting](#mounting)
- [Docker](#docker)
- [License](#license)

<!-- markdown-toc end -->

# Installation

This project is in early development and has not reached 1.0. You will have to
build the binary yourself:

```shell
go get github.com/soopsio/sshfs
env GOOS=linux go build github.com/soopsio/sshfs
```

# Usage

SSHFS is one binary that can mount keys or run a Docker volume plugin to do so
for containers. Run `sshfs --help` to see options not documented here.

## Mounting

```
Usage:
  sshfs mount {mountpoint} [flags]

Flags:
  -a, --address string    ssh server address (default "127.0.0.1:22")
  -h, --help              help for mount
  -p, --password string   ssh password
  -r, --root string       ssh root (default "/opt")
  -u, --username string   ssh username (default "root")
```

To mount secrets, first create a mountpoint (`mkdir test`), then use `sshfs`
to mount:

```shell
sshfs mount -a 10.10.10.10:22 -u root -p ****** --log-level debug -r /tmp/test /opt/data/tmp
```

## Docker

```
Usage:
  sshfs docker {mountpoint} [flags]

Flags:
  -a, --address string    ssh server address (default "127.0.0.1:22")
  -h, --help              help for docker
  -p, --password string   ssh password
  -s, --socket string     socket address to communicate with docker (default "/run/docker/plugins/ssh.sock")
  -u, --username string   ssh username (default "root")
```

To start the Docker plugin, create a directory to hold mountpoints (`mkdir
test`), then use `sshfs` to start the server. When Docker volumes request a
volume (`docker run --volume-driver vault --volume
{prefix}:/container/secret/path`), the plugin will create mountpoints and manage
FUSE servers automatically.

```shell
sshfs docker /mnt/sshfs -a 10.10.10.10:22 -u root -p ****** --log-level debug  -r /tmp/test
ls /run/docker/plugins/
ssh.sock
docker run --rm -it -v myvola:/data --volume-driver=ssh alpine sh
docker volume ls
DRIVER              VOLUME NAME
ssh                 myvola
```

# License

SSHFS is licensed under an
[Apache 2.0 License](http://www.apache.org/licenses/LICENSE-2.0.html) (see also:
[LICENSE](LICENSE))
