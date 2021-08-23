package libcore

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Dreamacro/clash/adapter/outbound"
	"github.com/Dreamacro/clash/constant"
	clashC "github.com/Dreamacro/clash/constant"
	"github.com/Dreamacro/clash/listener/socks"
	"github.com/pkg/errors"
	"io"
	"log"
	"net"
	"sync"
)

type ClashBasedInstance struct {
	access    sync.Mutex
	socksPort int
	ctx       chan constant.ConnContext
	in        *socks.Listener
	out       clashC.ProxyAdapter
	started   bool
}

func (s *ClashBasedInstance) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dest, err := addrToMetadata(address)
	if err != nil {
		return nil, err
	}
	dest.NetWork = networkForClash(network)
	return s.out.DialContext(ctx, dest)
}

func newClashBasedInstance(socksPort int, out clashC.ProxyAdapter) *ClashBasedInstance {
	return &ClashBasedInstance{
		socksPort: socksPort,
		ctx:       make(chan constant.ConnContext, 100),
		out:       out,
	}
}

func (s *ClashBasedInstance) Start() error {
	s.access.Lock()
	defer s.access.Unlock()

	if s.started {
		return errors.New("already started")
	}

	in, err := socks.New(fmt.Sprintf("127.0.0.1:%d", s.socksPort), s.ctx)
	if err != nil {
		return errors.WithMessage(err, "create socks inbound")
	}
	s.in = in
	s.started = true
	go s.loop()
	return nil
}

func (s *ClashBasedInstance) Close() error {
	s.access.Lock()
	defer s.access.Unlock()

	if !s.started {
		return errors.New("not started")
	}

	err := s.in.Close()
	if err != nil {
		return err
	}
	close(s.ctx)
	return nil
}

func (s *ClashBasedInstance) loop() {
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
			_ = remote.Close()
			_ = conn.Conn().Close()
		}()
	}
}

func addrToMetadata(rawAddress string) (addr *clashC.Metadata, err error) {
	host, port, err := net.SplitHostPort(rawAddress)
	if err != nil {
		err = fmt.Errorf("addrToMetadata failed: %w", err)
		return
	}

	ip := net.ParseIP(host)
	if ip == nil {
		addr = &clashC.Metadata{
			AddrType: clashC.AtypDomainName,
			Host:     host,
			DstIP:    nil,
			DstPort:  port,
		}
		return
	} else if ip4 := ip.To4(); ip4 != nil {
		addr = &clashC.Metadata{
			AddrType: clashC.AtypIPv4,
			Host:     "",
			DstIP:    ip4,
			DstPort:  port,
		}
		return
	}

	addr = &clashC.Metadata{
		AddrType: clashC.AtypIPv6,
		Host:     "",
		DstIP:    ip,
		DstPort:  port,
	}
	return
}

func networkForClash(network string) clashC.NetWork {
	switch network {
	case "tcp", "tcp4", "tcp6":
		return clashC.TCP
	case "udp", "udp4", "udp6":
		return clashC.UDP
	}
	log.Fatalln("unexpected network name", network)
	return 0
}

func NewShadowsocksInstance(socksPort int, server string, port int, password string, cipher string, plugin string, pluginOpts string) (*ClashBasedInstance, error) {
	if plugin == "obfs-local" || plugin == "simple-obfs" {
		plugin = "obfs"
	}
	opts := map[string]interface{}{}
	err := json.Unmarshal([]byte(pluginOpts), &opts)
	if err != nil {
		return nil, err
	}
	out, err := outbound.NewShadowSocks(outbound.ShadowSocksOption{
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
	return newClashBasedInstance(socksPort, out), nil
}

func NewShadowsocksRInstance(socksPort int, server string, port int, password string, cipher string, obfs string, obfsParam string, protocol string, protocolParam string) (*ClashBasedInstance, error) {
	out, err := outbound.NewShadowSocksR(outbound.ShadowSocksROption{
		Server:        server,
		Port:          port,
		Password:      password,
		Cipher:        cipher,
		Obfs:          obfs,
		ObfsParam:     obfsParam,
		Protocol:      protocol,
		ProtocolParam: protocolParam,
		UDP:           true,
	})
	if err != nil {
		return nil, err
	}
	return newClashBasedInstance(socksPort, out), nil
}

func NewSnellInstance(socksPort int, server string, port int, psk string, obfsMode string, obfsHost string, version int) (*ClashBasedInstance, error) {
	obfs := map[string]interface{}{}
	obfs["mode"] = obfsMode
	obfs["host"] = obfsHost
	out, err := outbound.NewSnell(outbound.SnellOption{
		Server:   server,
		Port:     port,
		Psk:      psk,
		Version:  version,
		ObfsOpts: obfs,
	})
	if err != nil {
		return nil, err
	}
	return newClashBasedInstance(socksPort, out), nil
}
