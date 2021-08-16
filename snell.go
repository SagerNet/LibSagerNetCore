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

type SnellInstance struct {
	access  sync.Mutex
	ctx     chan constant.ConnContext
	in      *socks.Listener
	out     *outbound.Snell
	started bool
}

func NewSnellInstance(socksPort int, server string, port int, psk string, obfsMode string, obfsHost string, version int) (*SnellInstance, error) {
	ctx := make(chan constant.ConnContext, 100)
	i, err := socks.New(fmt.Sprintf("127.0.0.1:%d", socksPort), ctx)
	if err != nil {
		return nil, err
	}
	obfs := map[string]interface{}{}
	obfs["Mode"] = obfsMode
	obfs["Host"] = obfsHost
	o, err := outbound.NewSnell(outbound.SnellOption{
		Server:   server,
		Port:     port,
		Psk:      psk,
		Version:  version,
		ObfsOpts: obfs,
	})
	if err != nil {
		return nil, err
	}
	return &SnellInstance{
		ctx: ctx,
		in:  i,
		out: o,
	}, nil
}

func (s *SnellInstance) Start() error {
	s.access.Lock()
	defer s.access.Unlock()

	if s.started {
		return errors.New("already started")
	}
	s.started = true
	go s.loop()
	return nil
}

func (s *SnellInstance) Close() error {
	s.access.Lock()
	defer s.access.Unlock()

	if !s.started {
		return errors.New("not started")
	}

	s.in.Close()
	close(s.ctx)
	return nil
}

func (s *SnellInstance) loop() {
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
