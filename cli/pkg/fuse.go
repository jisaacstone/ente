package pkg

import (
	"context"
	"log"
	"os"
	"syscall"

	"github.com/ente-io/cli/internal/api"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// inMemoryFS is the root of the tree
type inMemoryFS struct {
	fs.Inode
	Client *api.Client
}

// Ensure that we implement NodeOnAdder
var _ = (fs.NodeOnAdder)((*inMemoryFS)(nil))

var collections = [4]string{"one", "twp", "three", "foru"}

// OnAdd is called on mounting the file system. Use it to populate
// the file system tree.
func (root *inMemoryFS) OnAdd(ctx context.Context) {
	p := &root.Inode
	for _, dir := range collections {
		var d = p.NewPersistentInode(ctx, &fs.Inode{},
			fs.StableAttr{Mode: syscall.S_IFDIR})
		p.AddChild(dir, d, true)
	}
}

// This demonstrates how to build a file system in memory. The
// read/write logic for the file is provided by the MemRegularFile type.
func (c *ClICtrl) Mount() {
	// This is where we'll mount the FS
	mntDir, _ := os.MkdirTemp("", "")

	root := &inMemoryFS{}
	server, err := fs.Mount(mntDir, root, &fs.Options{
		MountOptions: fuse.MountOptions{Debug: true},
	})
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Mounted on %s", mntDir)
	log.Printf("Unmount by calling 'fusermount -u %s'", mntDir)

	// Wait until unmount before exiting
	server.Wait()
}
