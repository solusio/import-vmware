package ssh

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/klauspost/readahead"
	"github.com/pkg/sftp"
	"github.com/solusio/import-vmware/common"
	"golang.org/x/crypto/ssh"
	"io"
	"os"
	"path"
	"time"
)

type Credentials struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Login string `json:"login"`
	Key   string `json:"key"`
}

type Connection struct {
	sshClient *ssh.Client

	// sftpClient an SFTP connection, used for checking connectivity, listing files,
	// creating directories, downloading, and uploading files.
	sftpClient *sftp.Client

	credentials Credentials
}

func NewConnection(c Credentials) (*Connection, error) {
	con := &Connection{
		credentials: c,
	}

	var err error
	con.sshClient, con.sftpClient, err = con.connect()
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	return con, nil
}

func (c *Connection) connect() (*ssh.Client, *sftp.Client, error) {
	sshClient, err := dialSSH(c.credentials.Host, c.credentials.Port, c.credentials.Login, c.credentials.Key)
	if err != nil {
		return nil, nil, fmt.Errorf("dial SSH: %w", err)
	}

	sftpClient, err := sftp.NewClient(sshClient, c.getSFTPOpts()...)
	if err != nil {
		return nil, nil, fmt.Errorf("establish SFTP connection: %w", err)
	}
	return sshClient, sftpClient, nil
}

func (*Connection) getSFTPOpts() []sftp.ClientOption {
	return []sftp.ClientOption{
		sftp.UseConcurrentReads(true),
		sftp.MaxConcurrentRequestsPerFile(512),
		sftp.UseConcurrentWrites(true),
		// Beware of customizing max packet size here!
		// Max packet size can cause "connection lost" error.
	}
}

func (c *Connection) Close() error {
	if c.sftpClient != nil {
		if err := c.sftpClient.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Connection) Exec(cmd string) ([]byte, error) {
	session, err := c.sshClient.NewSession()
	if err != nil {
		return nil, err
	}
	defer common.CloseWrapper(session)

	return session.CombinedOutput(cmd)
}

func (c *Connection) List(_ context.Context, p string) ([]os.FileInfo, error) {
	// `ReadDir` method will return "file does not exist" error if `path` isn't
	// directory which is quite bad.
	// So we should check manually.
	i, err := c.sftpClient.Stat(p)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", p, err)
	}

	if !i.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %q", p)
	}

	ii, err := c.sftpClient.ReadDir(p)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	return ii, nil
}

func (c *Connection) Upload(ctx context.Context, r io.Reader, dstPath, name string) error {
	if err := c.ensureIsConnected(); err != nil {
		return fmt.Errorf("ensure is connected: %w", err)
	}

	// We should check that `dstPath` exist, and it is a directory, or we will
	// get pointless error message `file does not exist` :/.
	if err := c.makeSureDirectoryExists(dstPath); err != nil {
		return fmt.Errorf("make sure directory exists: %w", err)
	}

	p := path.Join(dstPath, name)

	return c.sftpUpload(ctx, r, p)
}

type negativeSizeReader struct {
	io.Reader
}

func (negativeSizeReader) Size() int64 {
	return -1
}

func (c *Connection) sftpUpload(ctx context.Context, r io.Reader, path string) error {
	fp, err := c.sftpClient.Create(path)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer common.CloseWrapper(fp)

	go func() {
		<-ctx.Done()
		// Maybe there is need to close file here as well: common.CloseWrapper(fp)
	}()

	ra, err := readahead.NewReaderSize(r, 4, 64*common.MiB) // check zstd
	if err != nil {
		return fmt.Errorf("new readahead reader: %w", err)
	}

	_, err = fp.ReadFrom(negativeSizeReader{ra}) // ReadFrom support concurrent reads.
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return err
}

func (c *Connection) makeSureDirectoryExists(p string) error {
	i, err := c.sftpClient.Stat(p)

	// Create directory if it's not exists.
	if os.IsNotExist(err) {
		if err := c.sftpClient.MkdirAll(p); err != nil {
			return fmt.Errorf("create: %w", err)
		}
		return nil
	}

	if err != nil {
		return err
	}

	if !i.IsDir() {
		return errors.New("not a directory")
	}
	return nil
}

func (c *Connection) IsExists(_ context.Context, path string) (bool, error) {
	_, err := c.sftpClient.Stat(path)

	if os.IsNotExist(err) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

func (c *Connection) ensureIsConnected() error {
	_, err := c.sftpClient.Getwd()
	if err == nil {
		return nil
	}

	if !errors.Is(err, sftp.ErrSSHFxConnectionLost) {
		return err
	}

	common.CloseWrapper(c)

	c.sshClient, c.sftpClient, err = c.connect()
	if err != nil {
		return fmt.Errorf("reconnect after connection loss: %w", err)
	}

	return nil
}

func (c *Connection) Remove(_ context.Context, p string) error {
	if c == nil {
		return fmt.Errorf("remove %q: sftpClient connection is nil", p)
	}
	if c.sftpClient == nil {
		return fmt.Errorf("remove %q: sftpClient client is nil", p)
	}
	return sftpRemove(c.sftpClient, p)
}

func (c *Connection) Download(ctx context.Context, srcPath string) (io.ReadCloser, error) {
	return c.sftpDownload(ctx, srcPath)
}

func (c *Connection) sftpDownload(ctx context.Context, srcPath string) (io.ReadCloser, error) {
	if err := c.ensureIsConnected(); err != nil {
		return nil, fmt.Errorf("ensure is connected: %w", err)
	}

	// In this case we will receive the same strange errors, so we should do by ourselves.
	i, err := c.sftpClient.Stat(srcPath)
	if err != nil {
		return nil, fmt.Errorf("invalid source path %q: %w", srcPath, err)
	}

	if i.IsDir() {
		return nil, fmt.Errorf("invalid source path %q: not a file", srcPath)
	}

	fp, err := c.sftpClient.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("sftpClient open file %q: %w", srcPath, err)
	}

	rx, tx := io.Pipe()

	go func() {
		<-ctx.Done()
		_ = tx.CloseWithError(ctx.Err())
	}()

	go func() {
		_, err := fp.WriteTo(tx)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		_ = tx.CloseWithError(err)
	}()

	return rx, nil
}

func dialSSH(host string, port int, login, key string) (*ssh.Client, error) {
	const timeout = 5 * time.Second

	cfg := &ssh.ClientConfig{
		User: login,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeysCallback(func() (signers []ssh.Signer, err error) {
				signer, err := ssh.ParsePrivateKey([]byte(key))
				if err != nil {
					return nil, err
				}

				if signer.PublicKey().Type() == ssh.KeyAlgoRSA {
					signers = append(signers,
						&wrappedSigner{
							Signer:    signer,
							algorithm: ssh.KeyAlgoRSASHA512,
						},
						&wrappedSigner{
							Signer:    signer,
							algorithm: ssh.KeyAlgoRSASHA256,
						},
					)
				}

				signers = append(signers, signer)

				return signers, nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // Because we can't relay on that.
		Timeout:         timeout,
	}

	return ssh.Dial("tcp", fmt.Sprintf("%s:%d", host, port), cfg)
}

// wrappedSigner wraps a signer and overrides its public key type with the provided
// algorithm.
// Taken from https://github.com/go-gitea/gitea/pull/17281.
type wrappedSigner struct {
	ssh.Signer
	algorithm string
}

func (s *wrappedSigner) PublicKey() ssh.PublicKey {
	return &wrappedPublicKey{
		PublicKey: s.Signer.PublicKey(),
		algorithm: s.algorithm,
	}
}

func (s *wrappedSigner) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	signer, ok := s.Signer.(ssh.AlgorithmSigner)
	if !ok {
		return nil, fmt.Errorf("invalid signer: ssh.AlgorithmSigner expected but %T given", s.Signer)
	}
	return signer.SignWithAlgorithm(rand, data, s.algorithm)
}

// wrappedPublicKey wraps a PublicKey and overrides its type.
// Taken from https://github.com/go-gitea/gitea/pull/17281.
type wrappedPublicKey struct {
	ssh.PublicKey
	algorithm string
}

func (k *wrappedPublicKey) Type() string {
	return k.algorithm
}

func sftpTouchFile(c *sftp.Client, p string) error {
	fp, err := c.Create(p)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}

	if err := fp.Close(); err != nil {
		return fmt.Errorf("close created file: %w", err)
	}
	return nil
}

func sftpRemove(c *sftp.Client, path string) error {
	i, err := c.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat: %w", err)
	}

	if !i.IsDir() {
		return c.Remove(path)
	}

	ii, err := c.ReadDir(path)
	if err != nil {
		return fmt.Errorf("read directory: %w", err)
	}

	for _, i := range ii {
		p := c.Join(path, i.Name())

		if i.IsDir() {
			err = sftpRemove(c, p)
		} else {
			err = c.Remove(p)
		}

		if err != nil {
			return fmt.Errorf("remove %q: %w", p, err)
		}
	}

	return c.Remove(path)
}

func readerWithBuffer(r io.Reader) io.Reader {
	// We should collect all data which will be sent or was received during SCP
	// file uploading/downloading process.

	// bufSize number was taken from `io.copyBuffer` default buffer size.
	const bufSize = 32 * 1024
	return bufio.NewReaderSize(r, bufSize)
}

func readCloserWithBuffer(r io.ReadCloser) io.ReadCloser {
	return bufReadCloser{
		r:      readerWithBuffer(r),
		closer: r.Close,
	}
}

type bufReadCloser struct {
	r      io.Reader
	closer func() error
}

var _ io.ReadCloser = bufReadCloser{}

func (r bufReadCloser) Read(p []byte) (int, error) {
	return r.r.Read(p)
}

func (r bufReadCloser) Close() error {
	return r.closer()
}
