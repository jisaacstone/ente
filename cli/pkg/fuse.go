package pkg

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"syscall"
	"time"

	"github.com/ente-io/cli/internal/api"
	eCrypto "github.com/ente-io/cli/internal/crypto"
	"github.com/ente-io/cli/pkg/model"
	"github.com/ente-io/cli/pkg/secrets"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// inMemoryFS is the root of the tree
type inMemoryFS struct {
	fs.Inode
	Client    *api.Client
	Account   model.Account
	KeyHolder *secrets.KeyHolder
}

type collectionNode struct {
	fs.Inode
	mtime time.Time
}

// Ensure that we implement NodeOnAdder
var _ = (fs.NodeOnAdder)((*inMemoryFS)(nil))

var _ = (fs.DirStream)((*collectionNode)(nil))

func (cn *collectionNode) DirStream(ctx context.Context) {
}
func (cn *collectionNode) Close() {
}
func (cn *collectionNode) HasNext() bool {
	return false
}
func (cn *collectionNode) Next() (fuse.DirEntry, syscall.Errno) {
	return fuse.DirEntry{}, syscall.EACCES
}

// OnAdd is called on mounting the file system. Use it to populate
// the file system tree.
func (root *inMemoryFS) OnAdd(ctx context.Context) {
	ctx = context.WithValue(ctx, "app", string(root.Account.App))
	ctx = context.WithValue(ctx, "account_key", root.Account.AccountKey())
	ctx = context.WithValue(ctx, "user_id", root.Account.UserID)
	p := &root.Inode
	collections, err := root.Client.GetCollections(ctx, 100)
	if err != nil {
		log.Panic(err)
	} else {
		for _, col := range collections {
			log.Printf("c %+v", col)
			var c = &collectionNode{}
			c.mtime = time.Unix(col.UpdationTime, 0)
			var d = p.NewPersistentInode(ctx, c,
				fs.StableAttr{Mode: syscall.S_IFDIR})
			collectionKey, err := root.KeyHolder.GetCollectionKey(ctx, col)
			decrName, err := eCrypto.SecretBoxOpenBase64(col.EncryptedName, col.NameDecryptionNonce, collectionKey)
			if err != nil {
				log.Fatalf("failed to decrypt collection name: %v", err)
			}
			p.AddChild(string(decrName), d, true)
		}
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
	root := &inMemoryFS{}
	root.Client = c.Client
	root.Account = account
	root.KeyHolder = c.KeyHolder
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
