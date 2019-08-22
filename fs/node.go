package fs

import (
	"encoding/json"
	kv "github.com/patrickmn/go-cache"
	"github.com/pkg/sftp"
	"github.com/sirupsen/logrus"
	sq "github.com/yireyun/go-queue"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
)

var ginode uint64 = 900000000
var freeInode = sq.NewQueue(1000000)
var inodeCache = kv.New(kv.DefaultExpiration, kv.NoExpiration)

// DebugServer hello world, the web server
func DebugServer(w http.ResponseWriter, req *http.Request) {
	jb, err := json.Marshal(&struct {
		FreeInode string             `json:"free_inode"`
		Count     int                `json:"count"`
		Items     map[string]kv.Item `json:"items"`
	}{
		FreeInode: freeInode.String(),
		Count:     inodeCache.ItemCount(),
		Items:     inodeCache.Items(),
	})
	if err != nil {
		io.WriteString(w, err.Error())
		return
	}
	io.WriteString(w, string(jb))
}

//func init() {
//	http.HandleFunc("/debug", DebugServer)
//	go func() {
//		err := http.ListenAndServe(":12345", nil)
//		if err != nil {
//			log.Fatal("ListenAndServe: ", err)
//		}
//	}()
//}

// InitInode inode
func InitInode(inode uint64) {
	ginode = inode
}

// genInode inode
func genInode() uint64 {
	//val, ok, _ := freeInode.Get()
	//if ok {
	//	return val.(uint64)
	//}
	ginode++
	return ginode
}

// Node 文件系统节点，用于描述目录或者文件的文件系统属性
type Node struct {
	inode     uint64
	name      string // 名称，如：test
	path      string // 远程服务器的目录，如："/tmp/test"
	localpath string // 本地绝对路径
	isdir     bool
	isroot    bool
	parent    *Node
	*File
	*Dir
	sftp *sftp.Client
}

// MarshalJSON 自定义序列化
func (n *Node) MarshalJSON() ([]byte, error) {
	type node struct {
		Name        string `json:"name"`
		Inode       uint64 `json:"inode"`
		ParentInode uint64 `json:"parent_inode"`
		ParentName  string `json:"parent_name"`
	}

	var s = struct {
		FileCount  int    `json:"files_count"`
		DirsCount  int    `json:"dirs_count"`
		Inode      uint64 `json:"inode"`
		Name       string `json:"name"`
		Parent     uint64 `json:"parent"`
		Type       string `json:"type"`
		LocalPath  string `json:"local_path"`
		RemotePath string `json:"remote_path"`
		Files      []node `json:"files,omitempty"`
		Dirs       []node `json:"dirs,omitempty"`
	}{
		Inode:     n.inode,
		Name:      n.name,
		LocalPath: n.LocalPath(),
		Parent: func() uint64 {
			if n.isroot {
				return 0
			}
			return n.parent.inode
		}(),
		FileCount: func() int {
			if n.Dir != nil && n.Dir.Files != nil {
				return len(*n.Dir.Files)
			}
			return 0
		}(),
		DirsCount: func() int {
			if n.Dir != nil && n.Dir.Dirs != nil {
				return len(*n.Dir.Dirs)
			}
			return 0
		}(),
		Type: func() string {
			if n.isdir {
				if n.isroot {
					return "dir:root"
				}
				return "dir"
			}
			return "file"
		}(),
		Files: func() []node {
			nodes := []node{}
			if n.Dir.Files != nil {
				for _, f := range *n.Dir.Files {
					nodes = append(nodes, node{
						Name:        f.name,
						Inode:       f.inode,
						ParentInode: f.parent.inode,
						ParentName:  f.parent.name,
					})
				}
			}
			return nodes
		}(),
		Dirs: func() []node {
			nodes := []node{}
			if n.Dir.Dirs != nil {
				for _, d := range *n.Dir.Dirs {
					nodes = append(nodes, node{
						Name:        d.name,
						Inode:       d.inode,
						ParentInode: d.parent.inode,
						ParentName:  d.parent.name,
					})
				}
			}
			return nodes
		}(),
		RemotePath: n.Path(),
	}
	return json.Marshal(&s)
}

// IsDir 判断是否目录
func (n *Node) IsDir() bool {
	return n.isdir
}

// IsRoot 判断是否根目录
func (n *Node) IsRoot() bool {
	return n.isroot
}

// GetInode 获取节点 inode
func (n *Node) GetInode() uint64 {
	return n.inode
}

// Save 缓存 FsNode
func (n *Node) Save() {
	inodeCache.Set(strconv.FormatUint(n.inode, 10), n, kv.NoExpiration)
	if n.parent != nil {
		key := strconv.FormatUint(n.parent.inode, 10) + "_" + n.name
		inodeCache.Set(key, n, kv.NoExpiration)
	}
}

// Path 获取节点绝对路径
// 从当前节点开始，一直向父节点循环
func (n *Node) Path() string {
	path := []string{n.name}
	pnode := Node{}
	if n.isroot {
		return n.path
	}
	pnode = *n.parent
	for !pnode.isroot {
		path = append(path, pnode.name)
		pnode = *pnode.parent
	}
	path = append(path, pnode.path)
	path = reverse(path)

	return filepath.Join(path...)
}

// LocalPath 获取本地路径
func (n *Node) LocalPath() string {
	path := []string{n.name}
	pnode := Node{}
	if n.isroot {
		return n.localpath
	}
	pnode = *n.parent
	for !pnode.isroot {
		path = append(path, pnode.name)
		pnode = *pnode.parent
	}
	path = append(path, pnode.localpath)
	path = reverse(path)

	return filepath.Join(path...)
}

// reverse 数组反转
func reverse(s []string) []string {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return s
}

// NewNode 新增节点
func NewNode(sftp *sftp.Client, inode uint64, parent *Node, name string, isdir, isroot bool) *Node {
	logrus.WithFields(map[string]interface{}{
		"inode":  inode,
		"name":   name,
		"isdir":  isdir,
		"isroot": isroot,
	}).Debugln("NewNode...")
	//debug.PrintStack()
	node := &Node{
		inode:  genInode(),
		File:   &File{},
		Dir:    &Dir{},
		sftp:   sftp,
		name:   name,
		isdir:  isdir,
		isroot: isroot,
		parent: parent,
	}
	node.Dir.Node = node
	node.File.Node = node
	if isdir && isroot {
		node.path = name
		node.name = filepath.Base(name)
	}

	if inode > 0 {
		node.inode = inode
	}
	node.Save()
	return node
}

// GetNodeByID 根据 id 获取 Node 对象
func GetNodeByID(inode uint64) (*Node, bool) {
	c, ok := inodeCache.Get(strconv.FormatUint(uint64(inode), 10))
	if !ok {
		return nil, ok
	}
	return c.(*Node), ok
}

// Rename Node
func (n *Node) Rename(onode, ndir *Node, nname string) {
	inodeCache.Delete(strconv.FormatUint(n.inode, 10) + "_" + onode.name)
	onode.parent = ndir
	onode.name = nname
	onode.Save()
}

// GetChild 根据名称获取子 Node
func (n *Node) GetChild(name string) (*Node, bool) {
	key := strconv.FormatUint(n.inode, 10) + "_" + name
	node, ok := inodeCache.Get(key)
	if !ok {
		return nil, ok
	}
	return node.(*Node), true
}

// Remove 删除 Node
func (n *Node) Remove() {
	inodeCache.Delete(strconv.FormatUint(n.inode, 10))
	if n.parent != nil {
		inodeCache.Delete(strconv.FormatUint(n.parent.inode, 10) + "_" + n.name)
	}
	n.rmInode()
}

// rmInode 释放inode
func (n *Node) rmInode() {
	_, _ = freeInode.Put(n.inode)
}
