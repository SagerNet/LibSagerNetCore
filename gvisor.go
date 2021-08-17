package libcore

import (
	"context"
	"errors"
	"fmt"
	"github.com/xjasonlyu/tun2socks/component/dialer"
	"github.com/xjasonlyu/tun2socks/constant"
	"github.com/xjasonlyu/tun2socks/core"
	"github.com/xjasonlyu/tun2socks/core/device/rwbased"
	"github.com/xjasonlyu/tun2socks/core/stack"
	"github.com/xjasonlyu/tun2socks/log"
	"github.com/xjasonlyu/tun2socks/proxy"
	"github.com/xjasonlyu/tun2socks/tunnel"
	"net"
	"os"
	"sync"
)

type Tun2socksG struct {
	access    sync.Mutex
	stack     *stack.Stack
	proxy     proxy.Proxy
	device    *rwbased.Endpoint
	router    string
	dns       string
	dnsAddr   *net.UDPAddr
	hijackDns bool
}

type proxyTunnel struct{}

func (*proxyTunnel) Add(conn core.TCPConn) {
	tunnel.Add(conn)
}
func (*proxyTunnel) AddPacket(packet core.UDPPacket) {
	tunnel.AddPacket(packet)
}

func NewTun2socksG(fd int, mtu int, socksPort int, router string, dnsPort int, hijackDns bool, debug bool) (*Tun2socksG, error) {
	file := os.NewFile(uintptr(fd), "")
	if file == nil {
		return nil, errors.New("failed to open TUN file descriptor")
	}

	tunDevice, err := rwbased.New(file, uint32(mtu))
	if err != nil {
		return nil, err
	}
	gvisorStack, err := stack.New(tunDevice, &proxyTunnel{}, stack.WithDefault())
	if debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.WarnLevel)
	}

	socks5Proxy, err := proxy.NewSocks5(fmt.Sprintf("127.0.0.1:%d", socksPort), "", "")
	if err != nil {
		return nil, err
	}

	dnsAddrStr := fmt.Sprintf("127.0.0.1:%d", dnsPort)
	dnsAddr, err := net.ResolveUDPAddr("udp", dnsAddrStr)
	if err != nil {
		return nil, err
	}

	tunnel.SetUDPTimeout(5 * 60)

	return &Tun2socksG{
		stack:     gvisorStack,
		device:    tunDevice,
		proxy:     socks5Proxy,
		router:    router,
		dns:       dnsAddrStr,
		dnsAddr:   dnsAddr,
		hijackDns: hijackDns,
	}, nil
}

func (t *Tun2socksG) Start() {
	t.access.Lock()
	defer t.access.Unlock()

	proxy.SetDialer(t)
}

func (t *Tun2socksG) Close() {
	t.access.Lock()
	defer t.access.Unlock()

	t.stack.Close()
}

func (t *Tun2socksG) DialContext(ctx context.Context, metadata *constant.Metadata) (net.Conn, error) {
	if metadata.DstIP.String() == t.router || metadata.DstPort == 53 && t.hijackDns {
		return dialer.DialContext(ctx, "tcp", t.dns)
	}
	return t.proxy.DialContext(ctx, metadata)
}

func (t *Tun2socksG) DialUDP(metadata *constant.Metadata) (net.PacketConn, error) {
	if metadata.DstIP.String() == t.router || t.hijackDns {
		return t.newDnsPacketConn(metadata)
	} else {
		return t.proxy.DialUDP(metadata)
	}
}

func (t *Tun2socksG) newDnsPacketConn(metadata *constant.Metadata) (conn net.PacketConn, err error) {
	conn, err = dialer.ListenPacket("udp", "")
	if err == nil {
		conn = &dnsPacketConn{conn: conn, dnsAddr: t.dnsAddr, realAddr: metadata.UDPAddr()}
	}
	return
}
