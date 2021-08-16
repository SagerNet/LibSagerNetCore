package libcore

import (
	"context"
	"errors"
	"fmt"
	"github.com/Dreamacro/clash/adapter/outbound"
	"github.com/Dreamacro/clash/constant"
	"github.com/Dreamacro/clash/listener/socks"
	"io"
	"sync"
)

type ShadowsocksRInstance struct {
	access  sync.Mutex
	ctx     chan constant.ConnContext
	in      *socks.Listener
	out     *outbound.ShadowSocksR
	started bool
}

func NewShadowsocksRInstance(socksPort int, server string, port int, password string, cipher string, obfs string, obfsParam string, protocol string, protocolParam string) (*ShadowsocksRInstance, error) {
	ctx := make(chan constant.ConnContext, 100)
	i, err := socks.New(fmt.Sprintf("127.0.0.1:%d", socksPort), ctx)
	if err != nil {
		return nil, err
	}
	o, err := outbound.NewShadowSocksR(outbound.ShadowSocksROption{
		Server:        server,
		Port:          port,
		Password:      password,
		Cipher:        cipher,
		Obfs:          obfs,
		ObfsParam:     obfsParam,
		Protocol:      protocol,
		ProtocolParam: protocolParam,
	})
	if err != nil {
		return nil, err
	}
	return &ShadowsocksRInstance{
		ctx: ctx,
		in:  i,
		out: o,
	}, nil
}

func (s *ShadowsocksRInstance) Start() error {
	s.access.Lock()
	defer s.access.Unlock()

	if s.started {
		return errors.New("already started")
	}
	s.started = true
	go s.loop()
	return nil
}

func (s *ShadowsocksRInstance) Close() error {
	s.access.Lock()
	defer s.access.Unlock()

	if !s.started {
		return errors.New("not started")
	}

	s.in.Close()
	close(s.ctx)
	return nil
}

func (s *ShadowsocksRInstance) loop() {
	for conn := range s.ctx {
		conn := conn
		metadata := conn.Metadata()
		go func() {
			remote, err := s.out.DialContext(context.Background(), metadata)
			if err != nil {
				fmt.Printf("Dial error: %s\n", err.Error())
				return
			}
			go func() {
				_, _ = io.Copy(remote, conn.Conn())
			}()
			_, _ = io.Copy(conn.Conn(), remote)
		}()
	}
}
