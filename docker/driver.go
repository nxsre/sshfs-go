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

package docker

import (
	"fmt"
	"github.com/getlantern/errors"
	"github.com/pkg/sftp"
	"github.com/soopsio/sshfs/fs"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/sirupsen/logrus"
)

type volumeName struct {
	name        string
	connections int
}

// Driver implements the interface for a Docker volume plugin
type Driver struct {
	config  Config
	servers map[string]*Server
	volumes map[string]*volumeName
	sftp    *sftp.Client
	m       *sync.Mutex
}

// New instantiates a new driver and returns it
func New(config Config) (*Driver, error) {
	client, err := fs.NewSftp(config.SSHConfig, config.SSHServer)
	if err != nil {
		return nil, err
	}
	return &Driver{
		sftp:    client,
		config:  config,
		servers: map[string]*Server{},
		m:       new(sync.Mutex),
	}, nil
}

// Create handles volume creation calls
func (d *Driver) Create(r *volume.CreateRequest) error {
	log.Println("Create Volume:")
	remotePath := filepath.Join(d.config.Root, r.Name)
	stat, err := d.sftp.Stat(remotePath)
	log.Println(remotePath, stat, err)
	if err != nil {
		if err == os.ErrNotExist {
			err = d.sftp.Mkdir(remotePath)
			log.Println(remotePath, stat, err)
			if err != nil {
				return err
			}
			stat, _ = d.sftp.Stat(remotePath)
			log.Println(remotePath, stat, err)
		} else {
			return err
		}
	}
	if !stat.IsDir() {
		return errors.New(remotePath + " not direcory!")
	}

	if err := os.MkdirAll(d.mountpoint(r.Name), os.ModeDir); err != nil {
		return err
	}

	if d.volumes == nil {
		d.volumes = map[string]*volumeName{}
	}
	d.volumes[d.mountpoint(r.Name)] = &volumeName{name: r.Name}
	return nil
}

// Get retrieves a volume
func (d *Driver) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	log.Println("Get Volume:", r)
	d.m.Lock()
	defer d.m.Unlock()
	m := d.mountpoint(r.Name)
	if s, ok := d.volumes[m]; ok {
		return &volume.GetResponse{Volume: &volume.Volume{Name: s.name, Mountpoint: d.mountpoint(s.name)}}, nil
	}

	return &volume.GetResponse{}, fmt.Errorf("Unable to find volume mounted on %s", m)
}

// List mounted volumes
func (d *Driver) List() (*volume.ListResponse, error) {
	log.Println("List Volume")
	d.m.Lock()
	defer d.m.Unlock()
	var vols []*volume.Volume
	for _, v := range d.volumes {
		vols = append(vols, &volume.Volume{Name: v.name, Mountpoint: d.mountpoint(v.name)})
	}
	return &volume.ListResponse{Volumes: vols}, nil
}

// Remove handles volume removal calls
func (d *Driver) Remove(r *volume.RemoveRequest) error {
	d.m.Lock()
	defer d.m.Unlock()
	mount := d.mountpoint(r.Name)
	logger := logrus.WithFields(logrus.Fields{
		"name":       r.Name,
		"mountpoint": mount,
	})
	logger.Debug("got remove request")

	if server, ok := d.servers[mount]; ok {
		if server.connections <= 1 {
			logger.Debug("removing server")
			delete(d.servers, mount)
		}
	}

	log.Println(d.servers)
	return nil
}

// Path handles calls for mountpoints
func (d *Driver) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	return &volume.PathResponse{Mountpoint: d.mountpoint(r.Name)}, nil
}

// Mount handles creating and mounting servers
func (d *Driver) Mount(r *volume.MountRequest) (*volume.MountResponse, error) {
	log.Println("88888888888888")
	d.m.Lock()
	defer d.m.Unlock()

	mount := d.mountpoint(r.Name)
	logger := logrus.WithFields(logrus.Fields{
		"name":       r.Name,
		"mountpoint": mount,
	})
	logger.Info("mounting volume")

	server, ok := d.servers[mount]
	if ok && server.connections > 0 {
		server.connections++
		return &volume.MountResponse{Mountpoint: mount}, nil
	}

	mountInfo, err := os.Lstat(mount)

	if os.IsNotExist(err) {
		if err := os.MkdirAll(mount, os.ModeDir|0444); err != nil {
			logger.WithError(err).Error("error making mount directory")
			return &volume.MountResponse{}, err
		}
	} else if err != nil {
		logger.WithError(err).Error("error checking if directory exists")
		return &volume.MountResponse{}, err
	}

	if mountInfo != nil && !mountInfo.IsDir() {
		logger.Error("already exists and not a directory")
		return &volume.MountResponse{}, fmt.Errorf("%s already exists and is not a directory", mount)
	}

	server, err = NewServer(d.config.SSHConfig, mount, d.config.SSHServer, filepath.Join(d.config.Root, r.Name))
	if err != nil {
		logger.WithError(err).Error("error creating server")
		return &volume.MountResponse{}, err
	}

	go server.Mount()
	d.servers[mount] = server

	return &volume.MountResponse{Mountpoint: mount}, nil
}

// Unmount handles unmounting (but not removing) servers
func (d *Driver) Unmount(r *volume.UnmountRequest) error {
	d.m.Lock()
	defer d.m.Unlock()

	mount := d.mountpoint(r.Name)
	logger := logrus.WithFields(logrus.Fields{
		"name":       r.Name,
		"mountpoint": mount,
	})
	logger.Info("unmounting volume")

	if server, ok := d.servers[mount]; ok {
		logger.WithField("conns", server.connections).Debug("found server")
		if server.connections == 1 {
			logger.Debug("unmounting")
			err := server.Unmount()
			if err != nil {
				logger.WithError(err).Error("error unmounting server")
				return err
			}
			server.connections--
		}
	} else {
		logger.Error("could not find volume")
		return fmt.Errorf("unable to find the volume mounted at %s", mount)
	}

	d.sftp.RemoveDirectory(filepath.Join(d.config.Root, r.Name))
	d.sftp.Close()
	return nil
}

func (d *Driver) mountpoint(name string) string {
	return path.Join(d.config.MountPoint, url.QueryEscape(name))
}

// Capabilities Driver
func (d *Driver) Capabilities() *volume.CapabilitiesResponse {
	return &volume.CapabilitiesResponse{Capabilities: volume.Capability{Scope: "local"}}
}

// Stop stops all the servers
func (d *Driver) Stop() []error {
	d.m.Lock()
	defer d.m.Unlock()
	logrus.Debug("got stop request")

	errs := []error{}
	for _, server := range d.servers {
		err := server.Unmount()
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}
