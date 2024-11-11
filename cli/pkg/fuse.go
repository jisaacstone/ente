package pkg

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/viper"

	"github.com/ente-io/cli/internal/api"
	"github.com/ente-io/cli/internal/crypto"
	"github.com/ente-io/cli/pkg/mapper"
	"github.com/ente-io/cli/pkg/model"
	"github.com/ente-io/cli/utils/constants"
	"github.com/ente-io/cli/utils/encoding"
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
	album  model.RemoteAlbum
	root   *inMemoryFS
	client resty.Client
}

const (
	downloadHost                = "https://files.ente.io/?fileID="
	decryptionBufferSize        = 4 * 1024 * 1024
	XChaCha20Poly1305IetfABYTES = 16 + 1
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
var _ = (fs.NodeOpener)((*imageNode)(nil))
var _ = (fs.NodeGetattrer)((*imageNode)(nil))

func DecryptFile(reader io.Reader, out io.Writer, key, nonce []byte) error {

	decryptor, err := crypto.NewDecryptor(key, nonce)
	if err != nil {
		return err
	}

	buf := make([]byte, decryptionBufferSize+XChaCha20Poly1305IetfABYTES)
	for {
		readCount, err := reader.Read(buf)
		if err != nil && err != io.EOF {
			log.Println("Failed to read from input file", err)
			return err
		}
		if readCount == 0 {
			break
		}
		n, tag, errErr := decryptor.Pull(buf[:readCount])
		if errErr != nil && errErr != io.EOF {
			log.Println("Failed to read from decoder", errErr)
			return errErr
		}

		if _, err := out.Write(n); err != nil {
			log.Println("Failed to write to output file", err)
			return err
		}
		if errErr == io.EOF {
			break
		}
		if tag == crypto.TagFinal {
			break
		}
	}
	return nil
}

func (in *imageNode) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Size = uint64(in.file.Info.FileSize)
	out.Mtime = uint64(in.file.LastUpdateTime)
	return 0
}

func (in *imageNode) Open(ctx context.Context, openFlags uint32) (fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	// disallow writes
	if fuseFlags&(syscall.O_RDWR|syscall.O_WRONLY) != 0 {
		return nil, 0, syscall.EROFS
	}
	return in, fuse.FOPEN_KEEP_CACHE, 0
}

func (in *imageNode) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	ctx = context.WithValue(ctx, "app", string(in.root.account.App))
	ctx = context.WithValue(ctx, "account_key", in.root.account.AccountKey())
	ctx = context.WithValue(ctx, "user_id", in.root.account.UserID)

	var url = downloadUrl(in.file.ID)
	log.Print(url)
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
	var fileBytes = bytes.NewBuffer(make([]byte, in.file.Info.FileSize))
	err = DecryptFile(
		body,
		fileBytes,
		in.file.Key.MustDecrypt(in.root.c.KeyHolder.DeviceKey),
		encoding.DecodeBase64(in.file.FileNonce),
	)
	if err != nil {
		return nil, syscall.EIO
	}

	return fuse.ReadResultData(fileBytes.Bytes()[off : int(off)+len(dest)]), 0
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
		go addImages(ctx, root, d, c)
	}
}

func addImages(ctx context.Context, root *inMemoryFS, iNode *fs.Inode, cn *collectionNode) {
	// Add images
	var hasNext = true
	var err error
	var files []api.File
	client := resty.New()
	for hasNext {
		files, hasNext, err = root.c.Client.GetFiles(ctx, cn.album.ID, 0)
		if err != nil {
			log.Panic(err)
		}
		for n, file := range files {
			if file.IsDeleted || file.IsRemovedFromAlbum() {
				continue
			}
			photoFile, err := mapper.MapApiFileToPhotoFile(ctx, cn.album, file, root.c.KeyHolder)
			if err != nil {
				log.Printf("err %v", err)
				continue
			}
			var i = &imageNode{file: photoFile, root: root, client: *client}
			var f = iNode.NewPersistentInode(ctx, i, fs.StableAttr{Mode: syscall.S_IFREG, Ino: uint64(file.ID)})
			iNode.AddChild(photoFile.GetTitle(), f, true)
			if n > 150 {
				break
			}
		}
		break
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
