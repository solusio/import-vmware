package ssh

import (
	"context"
	"fmt"
	"github.com/kardianos/osext"
	"github.com/solusio/import-vmware/common"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

const (
	agentTmpPath = "/tmp/import-agent"
)

type NodeConnection struct {
	credentials Credentials
	sshConn     *Connection
}

func NewNodeConnection(host string, port int, login string, privateKeyPath string) (NodeConnection, error) {
	privateKey, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return NodeConnection{}, fmt.Errorf("failed to read private key file %q: %w", privateKeyPath, err)
	}

	c := Credentials{
		Host:  host,
		Port:  port,
		Login: login,
		Key:   string(privateKey),
	}

	con, err := NewConnection(c)
	if err != nil {
		return NodeConnection{}, err
	}

	return NodeConnection{
		credentials: c,
		sshConn:     con,
	}, nil
}

func (n NodeConnection) Exec(cmd string) ([]byte, error) {
	log.Printf("[%s] start execute command %q", n.credentials.Host, cmd)
	defer log.Printf("[%s] command executed %q", n.credentials.Host, cmd)
	return n.sshConn.Exec(cmd)
}

func (n NodeConnection) ExecAgent(args string) ([]byte, error) {
	cmd := fmt.Sprintf("%s %s", agentTmpPath, args)
	return n.Exec(cmd)
}

func (n NodeConnection) UploadAgent() error {
	log.Printf("[%s] start upload agent", n.credentials.Host)

	execPath, err := osext.Executable()
	if err != nil {
		return fmt.Errorf("get path of current executable: %w", err)
	}
	tmpDir, err := os.MkdirTemp(os.TempDir(), "import-agent")
	if err != nil {
		return err
	}
	tmpPath := filepath.Join(tmpDir, "import-agent")
	if err := common.Copy(execPath, tmpPath); err != nil {
		return err
	}
	_, _ = n.Exec(fmt.Sprintf("pkill -9 %s", agentTmpPath))

	f, err := os.OpenFile(tmpPath, os.O_RDONLY, 0666)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10+time.Minute)
	defer cancel()
	if err := n.sshConn.Upload(ctx, f, filepath.Dir(agentTmpPath), filepath.Base(agentTmpPath)); err != nil {
		return fmt.Errorf("copy agent file %q to node %s:%s: %w", tmpPath, n.credentials.Host, agentTmpPath, err)
	}

	if _, err := n.Exec(fmt.Sprintf("chmod +x %s", agentTmpPath)); err != nil {
		return err
	}

	log.Printf("[%s] agent uploaded", n.credentials.Host)
	time.Sleep(time.Second)
	return nil
}

func (n NodeConnection) DownloadFile(remotePath, localPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 1+time.Minute)
	defer cancel()
	r, err := n.sshConn.Download(ctx, remotePath)
	if err != nil {
		return err
	}
	defer common.CloseWrapper(r)

	b, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read file %q: %w", remotePath, err)
	}

	return os.WriteFile(localPath, b, 0644)
}
