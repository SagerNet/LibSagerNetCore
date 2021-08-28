package libcore

import (
	"context"
	"fmt"
	"github.com/Dreamacro/clash/adapter/outbound"
	"github.com/Dreamacro/clash/component/dialer"
	clashC "github.com/Dreamacro/clash/constant"
	"github.com/xjasonlyu/tun2socks/log"
	"github.com/xjasonlyu/tun2socks/transport/socks4"
	"net"
	"strconv"
	"time"
)

type socks4To5Instance struct {
	*outbound.Base
	username string
	socks4a  bool
}

func (s *socks4To5Instance) StreamConn(c net.Conn, metadata *clashC.Metadata) (net.Conn, error) {
	if metadata.Host != "" && !s.socks4a {
		addr, err := net.ResolveIPAddr("ip", metadata.Host)
		if err != nil {
			log.Debugf("socks4 resolve host %s error: %s", metadata.Host, err.Error())
		}
		metadata.Host = ""
		metadata.DstIP = addr.IP
	}
	err := socks4.ClientHandshake(c, metadata.RemoteAddress(), socks4.CmdConnect, s.username)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *socks4To5Instance) DialContext(ctx context.Context, metadata *clashC.Metadata) (_ clashC.Conn, err error) {
	c, err := dialer.DialContext(ctx, "tcp", s.Addr())
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %w", s.Addr(), err)
	}
	tcpKeepAlive(c)

	defer safeConnClose(c, err)

	c, err = s.StreamConn(c, metadata)
	if err != nil {
		return nil, err
	}

	return outbound.NewConn(c, s), nil
}

func tcpKeepAlive(c net.Conn) {
	if tcp, ok := c.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
	}
}

func safeConnClose(c net.Conn, err error) {
	if err != nil {
		_ = c.Close()
	}
}

func NewSocks4To5Instance(socksPort int32, serverAddress string, serverPort int32, username string, socks4a bool) (*ClashBasedInstance, error) {
	addr := net.JoinHostPort(serverAddress, strconv.Itoa(int(serverPort)))
	out := &socks4To5Instance{
		Base:     outbound.NewBase("", addr, -1, false),
		username: username,
		socks4a:  socks4a,
	}
	return newClashBasedInstance(socksPort, out), nil
}
