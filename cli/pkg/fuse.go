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
	"github.com/ente-io/cli/pkg/mapper"
	"github.com/ente-io/cli/pkg/model"
	"github.com/ente-io/cli/pkg/secrets"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// inMemoryFS is the root of the tree
type inMemoryFS struct {
	fs.Inode
	Client    *api.Client
	Account   *model.Account
	KeyHolder *secrets.KeyHolder
}

type collectionNode struct {
	fs.Inode
	mtime     time.Time
	Account   *model.Account
	Album     *model.RemoteAlbum
	Client    *api.Client
	KeyHolder *secrets.KeyHolder
	nameToId  map[string]int64
}

type imageNode struct {
	fs.Inode
	file api.File
}

type dirStream struct {
	ctx     context.Context
	next    *model.RemoteFile
	iterate func()
}

var _ = (fs.NodeOnAdder)((*inMemoryFS)(nil))
var _ fs.DirStream = (*dirStream)(nil)
var (
	_ fs.NodeLookuper  = (*collectionNode)(nil)
	_ fs.NodeReaddirer = (*collectionNode)(nil)
)

func (d *dirStream) HasNext() bool {
	log.Printf("HN")
	if d.next == nil {
		return false
	}
	return true
}

func (d *dirStream) Next() (_ fuse.DirEntry, errno syscall.Errno) {
	log.Printf("Nxt %v", d.next)
	if d.next != nil {
		var de = fuse.DirEntry{
			Name: d.next.GetTitle(),
			Mode: 400,
			Ino:  uint64(d.next.ID),
		}
		d.iterate()
		return de, fs.OK
	}
	return fuse.DirEntry{}, fs.ENOATTR
}

func (d *dirStream) Close() {
}

func (cn *collectionNode) Readdir(ctx context.Context) (_ fs.DirStream, errno syscall.Errno) {
	var hasMore bool = true
	var files []api.File
	var err error
	var index int = 0
	ctx = context.WithValue(ctx, "app", string(cn.Account.App))
	ctx = context.WithValue(ctx, "account_key", cn.Account.AccountKey())
	ctx = context.WithValue(ctx, "user_id", cn.Account.UserID)

	var ds = &dirStream{
		ctx: ctx,
	}

	log.Printf("ctx B %+v\naL: %+v, %+v", ctx, cn.Album, cn.Client)
	ds.iterate = func() {
		index++
		var next *api.File
		if index >= len(files) {
			if hasMore {
				files, hasMore, err = cn.Client.GetFiles(ctx, cn.Album.ID, 0)
				if err == nil {
					index = 0
					next = &files[index]
					log.Printf("nne=%v", next)
				}
			}
		} else {
			next = &files[index]
			log.Printf("ne=%v", next)
		}
		if next != nil {
			photoFile, err := mapper.MapApiFileToPhotoFile(ctx, *cn.Album, *next, cn.KeyHolder)
			log.Printf("f %+v e=%v", photoFile, err)
			cn.nameToId[photoFile.GetTitle()] = photoFile.ID
			if err == nil {
				ds.next = photoFile
			}
		}
	}
	ds.iterate()
	return ds, fs.OK
}

func (cn *collectionNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (_ *fs.Inode, errno syscall.Errno) {
	ctx = context.WithValue(ctx, "app", string(cn.Account.App))
	ctx = context.WithValue(ctx, "account_key", cn.Account.AccountKey())
	ctx = context.WithValue(ctx, "user_id", cn.Account.UserID)
	id := cn.nameToId[name]
	file, err := cn.Client.GetFile(ctx, cn.Album.ID, id)
	if err != nil {
		return nil, fs.ToErrno(err)
	}
	var fNode = &imageNode{
		file: *file,
	}
	var iN = fNode.NewInode(ctx, fNode, fs.StableAttr{Mode: out.Attr.Mode})
	return iN, fs.OK
}

// OnAdd is called on mounting the file system. Use it to populate
// the file system tree.
func (root *inMemoryFS) OnAdd(ctx context.Context) {
	ctx = context.WithValue(ctx, "app", string(root.Account.App))
	ctx = context.WithValue(ctx, "account_key", root.Account.AccountKey())
	ctx = context.WithValue(ctx, "user_id", root.Account.UserID)
	log.Printf("ctx A %+v", ctx)
	p := &root.Inode
	collections, err := root.Client.GetCollections(ctx, 100)
	if err != nil {
		log.Panic(err)
	}
	for _, col := range collections {
		album, err := mapper.MapCollectionToAlbum(ctx, col, root.KeyHolder)
		if err != nil {
			log.Panic(err)
		}
		log.Printf("c %+v", album)
		var c = &collectionNode{
			Album:     album,
			Account:   root.Account,
			Client:    root.Client,
			mtime:     time.Unix(col.UpdationTime, 0),
			KeyHolder: root.KeyHolder,
			nameToId:  make(map[string]int64),
		}
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
	root.Account = &account
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
