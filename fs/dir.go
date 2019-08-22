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
	"hash/crc64"
	"log"
	"os"
	"path"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	//krfs "github.com/kr/fs"
	"github.com/pkg/sftp"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/context"
)

var table = crc64.MakeTable(crc64.ISO)

// Dir implements both Node and Handle
type Dir struct {
	*Node
	Files *[]*File
	Dirs  *[]*Dir
	sync.Mutex
}

var _ fs.Node = (*Dir)(nil)

// NewRoot creates a new root and returns it
func NewRoot(root string, c *sftp.Client) *Node {
	rnode := NewNode(c, 0, nil, root, true, true)
	return rnode
}

var _ fs.NodeOpener = (*Dir)(nil)

// Open Dir
func (d *Dir) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	logrus.Debug("handling Dir.Open call")
	fh := &Dir{
		Node: d.Node,
	}

	fh.Lock()
	return fh, nil
}

var _ fs.NodeSetattrer = (*Dir)(nil)

// Setattr Dir
func (d *Dir) Setattr(ctx context.Context, req *fuse.SetattrRequest, resp *fuse.SetattrResponse) error {
	logrus.WithField("req", req).Debug("handling Dir.Setattr call")
	return nil
}

var _ fs.HandleReleaser = (*Dir)(nil)

// Release Dir
func (d *Dir) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	logrus.Debug("handling Dir.Release call", d.Path())
	d.Unlock()
	return nil
}

// Attr sets attrs on the given fuse.Attr
func (d *Dir) Attr(ctx context.Context, a *fuse.Attr) error {
	logrus.WithField("path", d.Path()).Debug("handling Dir.Attr call")
	stat, err := d.sftp.Stat(d.Path())
	if err != nil {
		return err
	}

	statT, ok := stat.Sys().(*sftp.FileStat)
	if ok {
		a.Atime = time.Unix(int64(statT.Atime), 0)
	}

	a.Inode = d.GetInode()
	a.Mode = stat.Mode()
	a.Mtime = stat.ModTime()
	a.Ctime = stat.ModTime()
	a.Size = 4096 // linux 文件系统目录的固定大小，每创建一个文件将分配 4096 字节
	return nil
}

var _ fs.NodeStringLookuper = (*Dir)(nil)

// Lookup looks up a path
func (d *Dir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	logrus.WithField("name", name).Debug("handling Dir.Lookup call")
	//time.Sleep(10 * time.Second)
	path := path.Join(d.Path(), name)

	childNode, ok := d.Node.GetChild(name)
	if ok {
		if childNode.isdir {
			return childNode.Dir, nil
		}
		return childNode.File, nil
	}

	// 本地缓存找不到对象则检查远程是否存在并添加到本地缓存
	f, err := d.sftp.Stat(path)
	logrus.Debugln("oooooo ", path, " ", err.Error())
	if err != nil {
		if err == os.ErrNotExist {
			return nil, fuse.ENOENT
		}
		return nil, err
	}
	// 本地没有，远程有时，本地创建节点
	childnode := NewNode(d.sftp, 0, d.Node, f.Name(), f.IsDir(), false)

	if f.IsDir() {
		directories := []*Dir{childnode.Dir}
		if d.Dirs != nil {
			directories = append(*d.Dirs, directories...)
		}
		d.Dirs = &directories
		return childnode.Dir, nil
	}
	files := []*File{childnode.File}
	if d.Files != nil {
		files = append(*d.Files, files...)
	}
	d.Files = &files
	return childnode.File, nil
}

var _ fs.NodeRemover = (*Dir)(nil)

// Remove Dir
func (d *Dir) Remove(ctx context.Context, req *fuse.RemoveRequest) error {
	logrus.WithField("current", d.Path()).WithField("req", req).Debug("handling Root.Remove call")
	path := filepath.Join(d.Path(), req.Name)
	rmnode, _ := d.GetChild(req.Name)

	if req.Dir {
		if rmnode.Dir.Dirs != nil {
			if len(*rmnode.Dirs) > 0 {
				return fuse.Errno(syscall.ENOTEMPTY)
			}
		}
		if rmnode.Dir.Files != nil {
			if len(*rmnode.Files) > 0 {
				return fuse.Errno(syscall.ENOTEMPTY)
			}
		}

		if err := d.sftp.RemoveDirectory(path); err != nil {
			return err
		}
		if rmnode != nil {
			rmnode.Remove()
		}
		newDirs := []*Dir{}
		for _, directory := range *d.Dirs {
			if directory.name != req.Name {
				newDirs = append(newDirs, directory)
			} else {
				if directory.Files != nil {
					return fuse.Errno(syscall.ENOTEMPTY)
				}
			}
		}
		d.Dirs = &newDirs
	} else {
		if err := d.sftp.Remove(path); err != nil {
			return err
		}

		if rmnode != nil {
			rmnode.Remove()
		}

		if d.Files != nil {
			newFiles := []*File{}
			for _, file := range *d.Files {
				if file.name != req.Name {
					newFiles = append(newFiles, file)
				}
			}
			d.Files = &newFiles
		}
	}

	return nil
}

var _ fs.HandleReadDirAller = (*Dir)(nil)

// ReadDirAll returns a list of sshfs
func (d *Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	logrus.WithField("dir", d).Debug("handling Dir.ReadDirAll call")
	//log.Println(d.name, d.path, d.isroot, d.Path())
	//d.Lock()
	//defer d.Lock()
	dirs := []fuse.Dirent{}
	fs, err := d.sftp.ReadDir(path.Join(d.Path()))
	if err != nil {
		return dirs, err
	}

	directories := []*Dir{}
	files := []*File{}

	for _, f := range fs {
		t := fuse.DT_File
		childnode, ok := d.Node.GetChild(f.Name())
		if !ok {
			childnode = NewNode(d.sftp, 0, d.Node, f.Name(), f.IsDir(), false)
		}
		if f.IsDir() {
			t = fuse.DT_Dir
			directories = append(directories, childnode.Dir)
		} else {
			files = append(files, childnode.File)
		}

		d := fuse.Dirent{
			Name:  childnode.name,
			Inode: childnode.inode,
			Type:  t,
		}
		dirs = append(dirs, d)
	}
	d.Node.Dir.Dirs = &directories
	d.Node.Dir.Files = &files
	return dirs, nil
}

var _ fs.NodeMkdirer = (*Dir)(nil)

// Mkdir Dir
func (d *Dir) Mkdir(ctx context.Context, req *fuse.MkdirRequest) (fs.Node, error) {
	logrus.Debug("handling Dir.Mkdir call")
	childnode, ok := d.GetChild(req.Name)
	if ok {
		if childnode.isdir {
			return childnode.Dir, nil
		}
		return childnode.File, nil
	}

	newNode := NewNode(d.sftp, 0, d.Node, req.Name, true, false)

	err := d.sftp.Mkdir(newNode.Path())
	if err != nil {
		return nil, err
	}

	err = d.sftp.Chmod(newNode.Path(), req.Mode)
	if err != nil {
		return nil, err
	}

	err = d.sftp.Chown(newNode.Path(), int(req.Uid), int(req.Gid))
	if err != nil {
		return nil, err
	}

	dirs := []*Dir{newNode.Dir}
	if d.Dirs != nil {
		dirs = append(*d.Dirs, dirs...)
	}
	d.Dirs = &dirs

	return newNode.Dir, nil
}

var _ fs.NodeCreater = (*Dir)(nil)

// Create Dir
func (d *Dir) Create(ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (fs.Node, fs.Handle, error) {
	logrus.Debug("handling Dir.Create call")
	node, ok := d.GetChild(req.Name)
	if ok {
		return node.File, node.File, nil
	}

	newNode := NewNode(d.sftp, 0, d.Node, req.Name, false, false)

	file, err := d.sftp.Create(newNode.Path())
	if err != nil {
		return nil, nil, err
	}

	err = d.sftp.Chmod(newNode.Path(), req.Mode)
	if err != nil {
		return nil, nil, err
	}

	err = d.sftp.Chown(newNode.Path(), int(req.Uid), int(req.Gid))
	if err != nil {
		return nil, nil, err
	}

	files := []*File{newNode.File}
	if d.Files != nil {
		files = append(*d.Files, files...)
	}
	d.Files = &files
	newNode.File.file = file
	newNode.File.Lock()
	return newNode.File, newNode.File, nil
}

// Rename Dir
func (d *Dir) Rename(ctx context.Context, req *fuse.RenameRequest, newDir fs.Node) error {
	log.Println("Rename requested from", req.OldName, "to", req.NewName)
	newParentNode := newDir.(*Dir).Node
	opath := filepath.Join(d.Path(), req.OldName)
	npath := filepath.Join(newParentNode.Path(), req.NewName)
	d.Lock()
	defer d.Unlock()

	// Rename 不改变 iNode
	onode, _ := d.GetChild(req.OldName)
	//log.Printf("OldParent: %s, OldName: %s, NewParent: %s, NewnName: %s", d.name, req.OldName, newParentNode.name, req.NewName)
	// # tree --inodes tmp
	//tmp/
	//|-- [8632662105]  dira
	//|   |-- [8632662109]  d1
	//|   |   `-- [8632662111]  a1
	//|   |-- [8632662110]  d2
	//|   `-- [8632662108]  t
	//`-- [8632662106]  dirb
	// test 为 RootName
	// 1. 同目录，不同名
	// # mv dira/t dira/t1
	// OldParent: dira, OldName: t, NewParent: dira, NewnName: t1
	// 2. 同名不同目录
	// mv tmp/dira/t tmp/dirb/t
	// OldParent: dira, OldName: t, NewParent: dirb, NewnName: t
	// 3. 文件不同名不同目录
	// mv tmp/dira/t tmp/dirb/t1
	// OldParent: dira, OldName: t, NewParent: dirb, NewnName: t1
	// 4. 非空目录不同目录同名
	// # mv tmp/dira tmp/dirb/
	// OldParent: test, OldName: dira, NewParent: dirb, NewnName: dira
	// 5. 非空目录同目录不同名
	// # mv tmp/dira tmp/dirc
	// OldParent: test, OldName: dira, NewParent: test, NewnName: dirc

	// onode 为当前要 rename 的对象节点（目录或文件），当前目录为 d.Node 为 onode.parent
	// newParentNode 新对象节点的父节点

	// 变更 Node 信息
	d.Node.Rename(onode, newParentNode, req.NewName)
	d.sftp.Rename(opath, npath)

	if newParentNode.inode == d.Node.inode {
		return nil
	}

	// 移动到新目录
	if onode.isdir {
		// 清除旧父节点记录
		if d.Dirs != nil {
			directories := []*Dir{}
			for _, dir := range *d.Dirs {
				if dir.inode != onode.inode {
					directories = append(directories, dir)
				}
			}
			d.Dirs = &directories
		}

		//新的父节点增加记录
		if newParentNode.Dirs != nil {
			directories := []*Dir{onode.Dir}
			for _, dir := range *d.Dirs {
				if dir.inode != onode.inode {
					directories = append(directories, dir)
				}
			}
			d.Dirs = &directories
		}

		return nil
	}
	// 清除旧父节点记录
	if d.Files != nil {
		files := []*File{}
		for _, file := range *d.Files {
			if file.inode != onode.inode {
				files = append(files, file)
			}
		}
		d.Files = &files
	}

	//新的父节点增加记录
	if newParentNode.Files != nil {
		files := []*File{onode.File}
		for _, file := range *d.Files {
			if file.inode != onode.inode {
				files = append(files, file)
			}
		}
		d.Files = &files
	}

	return nil
}

var _ fs.NodeSymlinker = (*Dir)(nil)

// Symlink Dir
func (d *Dir) Symlink(ctx context.Context, req *fuse.SymlinkRequest) (fs.Node, error) {
	logrus.WithField("req", req).Debugln("handling Dor.Symlink call")
	return d.Dir, nil
}

var _ fs.NodeLinker = (*Dir)(nil)

// Link Dir
func (d *Dir) Link(ctx context.Context, req *fuse.LinkRequest, old fs.Node) (fs.Node, error) {
	logrus.WithField("req", req).Debugln("handling Dir.Link call")
	return d.Dir, nil
}
