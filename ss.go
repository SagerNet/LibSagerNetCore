package libcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Dreamacro/clash/adapter/outbound"
	"github.com/Dreamacro/clash/constant"
	"github.com/Dreamacro/clash/listener/socks"
	"io"
	"sync"
)

type ShadowsocksInstance struct {
	access  sync.Mutex
	ctx     chan constant.ConnContext
	in      *socks.Listener
	out     *outbound.ShadowSocks
	started bool
}

func NewShadowsocksInstance(socksPort int, server string, port int, password string, cipher string, plugin string, pluginOpts string) (*ShadowsocksInstance, error) {
	ctx := make(chan constant.ConnContext, 100)
	i, err := socks.New(fmt.Sprintf("127.0.0.1:%d", socksPort), ctx)
	if err != nil {
		return nil, err
	}
	opts := map[string]interface{}{}
	err = json.Unmarshal([]byte(pluginOpts), &opts)
	if err != nil {
		return nil, err
	}
	o, err := outbound.NewShadowSocks(outbound.ShadowSocksOption{
		Server:     server,
		Port:       port,
		Password:   password,
		Cipher:     cipher,
		Plugin:     plugin,
		PluginOpts: opts,
	})
	if err != nil {
		return nil, err
	}
	return &ShadowsocksInstance{
		ctx: ctx,
		in:  i,
		out: o,
	}, nil
}

func (s *ShadowsocksInstance) Start() error {
	s.access.Lock()
	defer s.access.Unlock()

	if s.started {
		return errors.New("already started")
	}
	s.started = true
	go s.loop()
	return nil
}

func (s *ShadowsocksInstance) Close() error {
	s.access.Lock()
	defer s.access.Unlock()

	if !s.started {
		return errors.New("not started")
	}

	s.in.Close()
	close(s.ctx)
	return nil
}

func (s *ShadowsocksInstance) loop() {
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
