// Copyright © 2016 Asteris, LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fs

import (
	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"context"
	"errors"
	"github.com/pkg/sftp"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"log"
	"os"
	"syscall"
	"time"
)

// SSHFS is a ssh filesystem
type SSHFS struct {
	*sftp.Client
	root       string
	conn       *fuse.Conn
	mountpoint string
}

// NewSftp sftp
func NewSftp(config *ssh.ClientConfig, server string) (*sftp.Client, error) {
	conn, err := ssh.Dial("tcp", server, config)
	if err != nil {
		panic("Failed to dial: " + err.Error())
	}
	return sftp.NewClient(conn)
}

var _ fs.FS = (*SSHFS)(nil)

// New returns a new SSHFS
func New(config *ssh.ClientConfig, mountpoint, server, root string) (*SSHFS, error) {
	client, err := NewSftp(config, server)
	if err != nil {
		return nil, err
	}
	sshfs := &SSHFS{
		Client:     client,
		root:       root,
		mountpoint: mountpoint,
	}
	return sshfs, nil
}

// Mount the FS at the given mountpoint
func (v *SSHFS) Mount() error {
	var err error

	// 初始化 INode
	fileinfo, err := os.Stat(v.mountpoint)
	if err != nil {
		log.Fatalln(err)
	}

	stat, ok := fileinfo.Sys().(*syscall.Stat_t)
	if !ok {
		log.Fatalf("%+v", fileinfo.Sys())
	}
	InitInode(stat.Ino)

	v.conn, err = fuse.Mount(
		v.mountpoint,
		fuse.FSName("ssh"),
		fuse.VolumeName("ssh"),
		fuse.AsyncRead(),
		fuse.WritebackCache(),
		fuse.DefaultPermissions(),
		fuse.AllowDev(),
		fuse.AllowOther(),
		//fuse.AllowRoot(),
	)

	logrus.Debug("created conn")
	if err != nil {
		return err
	}

	logrus.Debug("starting to serve")
	return fs.Serve(v.conn, v)
}

// Unmount the FS
func (v *SSHFS) Unmount() error {
	if v.conn == nil {
		return errors.New("not mounted")
	}

	err := fuse.Unmount(v.mountpoint)
	if err != nil {
		return err
	}

	err = v.conn.Close()
	if err != nil {
		return err
	}

	logrus.Debug("closed connection, waiting for ready")
	<-v.conn.Ready
	if v.conn.MountError != nil {
		return v.conn.MountError
	}

	return nil
}

// Root returns the struct that does the actual work
func (v *SSHFS) Root() (fs.Node, error) {
	logrus.Debug("returning root")
	root := NewRoot(v.root, v.Client)
	root.localpath = v.mountpoint
	return root.Dir, nil
}

var _ fs.FSStatfser = (*SSHFS)(nil)

// Statfs sshfs
func (v *SSHFS) Statfs(ctx context.Context, req *fuse.StatfsRequest, resp *fuse.StatfsResponse) error {
	logrus.Debug("handling SSHFS.Statfs call")
	return nil
}

// timespecToTime
func timespecToTime(ts syscall.Timespec) time.Time {
	return time.Unix(int64(ts.Sec), int64(ts.Nsec))
}
