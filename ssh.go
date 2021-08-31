package libcore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"github.com/Dreamacro/clash/adapter/outbound"
	clashC "github.com/Dreamacro/clash/constant"
	"github.com/pkg/errors"
	"github.com/xjasonlyu/tun2socks/log"
	"golang.org/x/crypto/ssh"
	"net"
	"strconv"
	"sync"
)

type sshClient struct {
	*outbound.Base
	access     sync.Mutex
	client     *ssh.Client
	username   string
	auth       int32
	password   string
	publicKey  ssh.PublicKey
	privateKey ssh.Signer
}

func (s *sshClient) Close() error {
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *sshClient) StreamConn(_ net.Conn, _ *clashC.Metadata) (net.Conn, error) {
	panic("unimplemented")
}

func (s *sshClient) connect() (*ssh.Client, error) {
	if s.client != nil {
		return s.client, nil
	}

	s.access.Lock()
	defer s.access.Unlock()

	if s.client != nil {
		return s.client, nil
	}

	config := &ssh.ClientConfig{
		User: s.username,
	}

	if s.auth == 1 {
		config.Auth = []ssh.AuthMethod{ssh.Password(s.password)}
	} else if s.auth == 2 {
		config.Auth = []ssh.AuthMethod{ssh.PublicKeys(s.privateKey)}
	}

	if s.publicKey == nil {
		config.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	} else {
		config.HostKeyCallback = (&fixedHostKey{s.publicKey}).check
	}

	if s.publicKey != nil {
		config.HostKeyAlgorithms = []string{s.publicKey.Type()}
	}

	log.Debugf("ssh dial to %s", s.Addr())

	client, err := ssh.Dial("tcp", s.Addr(), config)
	if err != nil {
		err = errors.WithMessage(err, "connect ssh")
		log.Warnf("%v", err)
		return nil, err
	}

	log.Debugf("ssh conn success")

	go func() {
		err := client.Wait()
		log.Debugf("ssh connection closed: %v", err)
		s.client = nil
	}()

	s.client = client
	return client, nil
}

func (s *sshClient) DialContext(_ context.Context, metadata *clashC.Metadata) (_ clashC.Conn, err error) {
	client, err := s.connect()
	if err != nil {
		return nil, err
	}
	c, err := client.Dial(metadata.NetWork.String(), metadata.RemoteAddress())
	if err != nil {
		err = fmt.Errorf("%s connect error: %w", s.Addr(), err)
		log.Warnf("%v", err)
		return nil, err
	}

	tcpKeepAlive(c)
	defer safeConnClose(c, err)

	return outbound.NewConn(c, s), nil
}

const (
	authTypeNone = iota
	authTypePassword
	authTypePublicKey
)

func NewSSHInstance(socksPort int32, serverAddress string, serverPort int32, username string, auth int32, password string, pem string, passphrase string, pubKey string) (*ClashBasedInstance, error) {
	addr := net.JoinHostPort(serverAddress, strconv.Itoa(int(serverPort)))
	out := &sshClient{
		Base:     outbound.NewBase("", addr, -1, false),
		username: username,
		auth:     auth,
		password: password,
	}
	switch auth {
	case authTypePassword:
		out.password = password
	case authTypePublicKey:
		var signer ssh.Signer
		var err error
		if passphrase == "" {
			signer, err = ssh.ParsePrivateKey([]byte(pem))
		} else {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(pem), []byte(passphrase))
		}
		if err != nil {
			return nil, errors.WithMessage(err, "parse private key")
		}
		out.privateKey = signer
	}
	if pubKey != "" {
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubKey))
		if err != nil {
			if err != nil {
				return nil, errors.WithMessage(err, "parse public key")
			}
		}
		out.publicKey = key
	}
	return newClashBasedInstance(socksPort, out), nil
}

type fixedHostKey struct {
	key ssh.PublicKey
}

func (f *fixedHostKey) check(_ string, _ net.Addr, key ssh.PublicKey) error {
	if f.key == nil {
		return fmt.Errorf("ssh: required host key was nil")
	}
	if !bytes.Equal(key.Marshal(), f.key.Marshal()) {
		return fmt.Errorf("ssh: host key mismatch, server send %s %s", key.Type(), base64Encode(key.Marshal()))
	}
	return nil
}

func base64Encode(data []byte) string {
	b := bytes.Buffer{}
	w := base64.NewEncoder(base64.StdEncoding, &b)
	_, _ = w.Write(data)
	return hex.EncodeToString(b.Bytes())
}
