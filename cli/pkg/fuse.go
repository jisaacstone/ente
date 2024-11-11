package pkg

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strconv"
	"syscall"

	"github.com/ente-io/cli/internal/api"
	"github.com/ente-io/cli/pkg/model"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// inMemoryFS is the root of the tree
type inMemoryFS struct {
	fs.Inode
	c       *ClICtrl
	account *model.Account
}

type collectionNode struct {
	fs.Inode
	root *inMemoryFS
}

type imageNode struct {
	fs.Inode
	file api.File
}

var _ = (fs.NodeOnAdder)((*inMemoryFS)(nil))

// OnAdd is called on mounting the file system. Use it to populate
// the file system tree.
func (root *inMemoryFS) OnAdd(ctx context.Context) {
	ctx = context.WithValue(ctx, "app", string(root.account.App))
	ctx = context.WithValue(ctx, "account_key", root.account.AccountKey())
	ctx = context.WithValue(ctx, "user_id", root.account.UserID)
	p := &root.Inode

	// Add album folders
	albums, err := root.c.getRemoteAlbums(ctx)
	if err != nil {
		log.Panic(err)
	}
	var idMap = make(map[uint64]*fs.Inode)
	for _, album := range albums {
		if err != nil {
			log.Panic(err)
		}
		var c = &collectionNode{
			root: root,
		}
		var d = p.NewPersistentInode(ctx, c,
			fs.StableAttr{Mode: syscall.S_IFDIR, Ino: uint64(album.ID)})
		idMap[uint64(album.ID)] = d
		p.AddChild(album.AlbumName, d, true)
	}

	// Add images
	entries, err := root.c.getRemoteAlbumEntries(ctx)
	if err != nil {
		log.Panic(err)
	}
	for _, image := range entries {
		var albumNode = idMap[uint64(image.AlbumID)]
		var i = &imageNode{}
		var f = albumNode.NewPersistentInode(ctx, i, fs.StableAttr{Mode: syscall.S_IFREG, Ino: uint64(image.FileID)})
		albumNode.AddChild(strconv.FormatInt(image.FileID, 17), f, true)
	}
}

// This demonstrates how to build a file system in memory. The
// read/write logic for the file is provided by the MemRegularFile type.
func (c *ClICtrl) Mount() error {
	accounts, err := c.GetAccounts(context.Background())
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		fmt.Printf("No accounts to mount\n Add account using `account add` cmd\n")
		return nil
	}
	var account = accounts[0]
	secretInfo, err := c.KeyHolder.LoadSecrets(account)
	c.Client.AddToken(account.AccountKey(), base64.URLEncoding.EncodeToString(secretInfo.Token))
	if err != nil {
		return err
	}
	// This is where we'll mount the FS
	mntDir, _ := os.MkdirTemp("", "")
	root := &inMemoryFS{
		account: &account,
		c:       c,
	}
	server, err := fs.Mount(mntDir, root, &fs.Options{
		MountOptions: fuse.MountOptions{Debug: true},
	})
	if err != nil {
		return err
	}

	log.Printf("Mounted on %s", mntDir)
	log.Printf("Unmount by calling 'fusermount -u %s'", mntDir)

	// Wait until unmount before exiting
	server.Wait()
	return nil
}
