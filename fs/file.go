package fs

import (
	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"context"
	"github.com/pkg/sftp"
	"github.com/sirupsen/logrus"
	"log"
	"sync"
	"time"
)

// File Node
type File struct {
	*Node
	create  bool
	file    *sftp.File
	writing bool
	sync.Mutex
}

var _ fs.Node = (*File)(nil)

// Attr File
func (f *File) Attr(ctx context.Context, a *fuse.Attr) error {
	logrus.Debug("handling File.Attr call")
	stat, err := f.sftp.Stat(f.Path())
	if err != nil {
		return err
	}

	statT, ok := stat.Sys().(*sftp.FileStat)
	if ok {
		a.Atime = time.Unix(int64(statT.Atime), 0)
	}

	a.Inode = f.GetInode()
	a.Mode = stat.Mode()
	a.Size = uint64(stat.Size())
	a.Ctime = stat.ModTime()
	a.Mtime = stat.ModTime()
	return nil
}

var _ fs.NodeSetattrer = (*File)(nil)

// Setattr File
func (f *File) Setattr(ctx context.Context, req *fuse.SetattrRequest, resp *fuse.SetattrResponse) error {
	logrus.WithField("req", req).Debug("handling File.Setattr call")
	if req.Valid.Size() {
		resp.Attr.Size = req.Size
		return f.sftp.Truncate(f.Path(), int64(req.Size))
	}
	return nil
}

var _ fs.NodeOpener = (*File)(nil)

// Open File
func (f *File) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	logrus.WithField("req", req).Debug("handling File.Open call")
	// Unsupported flags
	if req.Flags&fuse.OpenAppend == fuse.OpenAppend {
		return nil, fuse.ENOTSUP
	}

	file, err := f.sftp.OpenFile(f.Path(), int(req.Flags))
	if err != nil {
		return nil, err
	}

	fh := &File{
		file: file,
		Node: f.Node,
	}
	fh.Lock()

	if req.Flags.IsReadOnly() {
		return fh, err
	}

	if req.Flags.IsWriteOnly() {
		resp.Flags = fuse.OpenPurgeAttr
		return fh, nil
	}

	return nil, fuse.ENOTSUP
}

var _ fs.Handle = (*File)(nil)

var _ fs.HandleReader = (*File)(nil)

// Read File
func (f *File) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	logrus.WithField("req", req).Debug("handling File.Read call")
	// TODO: 大文件按照 req.Offset 分块读取
	if f.file == nil {
		var err error
		f.file, err = f.sftp.OpenFile(f.Path(), int(req.Flags))
		if err != nil {
			return err
		}
	}

	f.file.Seek(req.Offset, 0)
	resp.Data = make([]byte, req.Size)
	f.file.Read(resp.Data)
	return nil
}

var _ fs.HandleWriter = (*File)(nil)

// Write File
func (f *File) Write(ctx context.Context, req *fuse.WriteRequest, resp *fuse.WriteResponse) error {
	logrus.Debug("handling File.Write call")
	var err error
	if f.file == nil {
		f.file, err = f.sftp.OpenFile(f.Path(), int(req.FileFlags)|int(req.Flags))
		if err != nil {
			return err
		}
	}
	//stat, err := f.file.Stat()
	_, err = f.file.Write(req.Data)
	resp.Size = len(req.Data)
	return err
}

var _ fs.NodeFsyncer = (*File)(nil)

// Fsync File
func (f *File) Fsync(ctx context.Context, req *fuse.FsyncRequest) error {
	logrus.Debug("handling File.Fsync call")
	return nil
}

var _ fs.HandleReleaser = (*File)(nil)

// Release File
func (f *File) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	logrus.Debug("handling File.Release call", f.Path())
	var err error
	if f.file != nil {
		err = f.file.Close()
	}
	f.Unlock()
	return err
}

var _ fs.HandleFlusher = (*File)(nil)

// Flush File
func (f *File) Flush(ctx context.Context, req *fuse.FlushRequest) error {
	log.Println("Flushing file", f.Path())
	return nil
}
