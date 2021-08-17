package libcore

import (
	"context"
	"errors"
	"fmt"
	"github.com/Dreamacro/clash/component/dialer"
	"github.com/Dreamacro/clash/constant"
	"github.com/Dreamacro/clash/listener/socks"
	"github.com/xjasonlyu/tun2socks/log"
	"github.com/xjasonlyu/tun2socks/transport/socks4"
	"io"
	"net"
	"sync"
)

type Socks4To5Instance struct {
	access   sync.Mutex
	address  string
	username string
	socks4a  bool
	ctx      chan constant.ConnContext
	in       *socks.Listener
	started  bool
}

func NewSocks4To5Instance(socksPort int, serverAddress string, serverPort int, username string, socks4a bool) (*Socks4To5Instance, error) {
	ctx := make(chan constant.ConnContext, 100)
	i, err := socks.New(fmt.Sprintf("127.0.0.1:%d", socksPort), ctx)
	if err != nil {
		return nil, err
	}
	return &Socks4To5Instance{
		ctx:      ctx,
		in:       i,
		address:  net.JoinHostPort(serverAddress, string(rune(serverPort))),
		username: username,
		socks4a:  socks4a,
	}, nil
}

func (s *Socks4To5Instance) Start() error {
	s.access.Lock()
	defer s.access.Unlock()

	if s.started {
		return errors.New("already started")
	}
	s.started = true
	go s.loop()
	return nil
}

func (s *Socks4To5Instance) Close() error {
	s.access.Lock()
	defer s.access.Unlock()

	if !s.started {
		return errors.New("not started")
	}

	s.in.Close()
	close(s.ctx)
	return nil
}

func (s *Socks4To5Instance) loop() {
	for conn := range s.ctx {
		conn := conn
		metadata := conn.Metadata()
		go func() {
			remote, err := dialer.DialContext(context.Background(), "tcp", s.address)
			if err != nil {
				log.Debugf("socks4 connect error: %s", err.Error())
				return
			}
			if metadata.Host != "" && !s.socks4a {
				addr, err := net.ResolveIPAddr("ip", metadata.Host)
				if err != nil {
					log.Debugf("socks4 resolve host %s error: %s", metadata.Host, err.Error())
				}
				metadata.Host = ""
				metadata.DstIP = addr.IP
			}
			err = socks4.ClientHandshake(remote, metadata.RemoteAddress(), socks4.CmdConnect, s.username)
			if err != nil {
				log.Debugf("socks4 handshake error: %s", err.Error())
				return
			}
			go func() {
				_, _ = io.Copy(remote, conn.Conn())
			}()
			_, _ = io.Copy(conn.Conn(), remote)
		}()
	}
}
