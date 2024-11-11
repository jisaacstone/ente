package pkg

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/spf13/viper"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/ente-io/cli/pkg/mapper"
	"github.com/ente-io/cli/pkg/model"
	"github.com/ente-io/cli/utils/constants"
	"github.com/go-resty/resty/v2"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// inMemoryFS is the root of the tree
type inMemoryFS struct {
	fs.Inode
	c       *ClICtrl
	account *model.Account
	token   string
}

type collectionNode struct {
	fs.Inode
	root  *inMemoryFS
	album model.RemoteAlbum
	files []*int
}

type imageNode struct {
	fs.Inode
	file   *model.RemoteFile
	root   *inMemoryFS
	client resty.Client
}

var (
	downloadHost = "https://files.ente.io/?fileID="
)

func downloadUrl(fileID int64) string {
	apiEndpoint := viper.GetString("endpoint.api")
	if apiEndpoint == "" || strings.Compare(apiEndpoint, constants.EnteApiUrl) == 0 {
		return downloadHost + strconv.FormatInt(fileID, 10)
	}
	return apiEndpoint + "/files/download/" + strconv.FormatInt(fileID, 10)
}

var _ = (fs.NodeOnAdder)((*inMemoryFS)(nil))
var _ = (fs.FileReader)((*imageNode)(nil))

func (in *imageNode) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	ctx = context.WithValue(ctx, "app", string(in.root.account.App))
	ctx = context.WithValue(ctx, "account_key", in.root.account.AccountKey())
	ctx = context.WithValue(ctx, "user_id", in.root.account.UserID)

	var url = downloadUrl(in.file.ID)
	req := in.client.R().
		SetContext(ctx).
		SetHeader("X-Auth-Token", in.root.token).
		SetDoNotParseResponse(true)
	r, err := req.Get(url)
	if err != nil {
		return nil, syscall.EIO
	}

	body := r.RawBody()
	defer body.Close()
	end := off + int64(len(dest))
	var fileBytes = make([]byte, end)
	body.Read(fileBytes)
	if end > int64(len(fileBytes)) {
		end = int64(len(fileBytes))
	}

	return fuse.ReadResultData(fileBytes[off:end]), 0
}

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
	var idMap = make(map[uint64]*collectionNode)
	for _, album := range albums {
		if album.IsDeleted {
			continue
		}
		if err != nil {
			log.Panic(err)
		}
		var c = &collectionNode{
			root:  root,
			album: album,
		}
		var d = p.NewPersistentInode(ctx, c,
			fs.StableAttr{Mode: syscall.S_IFDIR, Ino: uint64(album.ID)})
		idMap[uint64(album.ID)] = c
		p.AddChild(album.AlbumName, d, true)
	}

	// Add images
	entries, err := root.c.getRemoteAlbumEntries(ctx)
	client := resty.New()
	if err != nil {
		log.Panic(err)
	}
	for n, image := range entries {
		var cn = idMap[uint64(image.AlbumID)]
		var cnNode = cn.EmbeddedInode()
		file, err := root.c.Client.GetFile(ctx, image.AlbumID, image.FileID)
		if err != nil {
			log.Println(err)
			continue
		}
		photoFile, err := mapper.MapApiFileToPhotoFile(ctx, cn.album, *file, root.c.KeyHolder)
		var i = &imageNode{file: photoFile, root: root, client: *client}
		log.Printf("file %+v", photoFile.Metadata)
		var f = cnNode.NewPersistentInode(ctx, i, fs.StableAttr{Mode: syscall.S_IFREG, Ino: uint64(image.FileID)})
		cnNode.AddChild(photoFile.GetTitle(), f, true)
		if n > 150 {
			break
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
	token := base64.URLEncoding.EncodeToString(secretInfo.Token)
	c.Client.AddToken(account.AccountKey(), token)
	if err != nil {
		return err
	}
	// This is where we'll mount the FS
	mntDir, _ := os.MkdirTemp("", "")
	root := &inMemoryFS{
		account: &account,
		token:   token,
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
