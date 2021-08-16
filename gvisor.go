package libcore

import (
	"context"
	"errors"
	"fmt"
	"github.com/SagerNet/go-tun2socks/log"
	"github.com/xjasonlyu/tun2socks/component/dialer"
	"github.com/xjasonlyu/tun2socks/constant"
	"github.com/xjasonlyu/tun2socks/core"
	"github.com/xjasonlyu/tun2socks/core/device/rwbased"
	"github.com/xjasonlyu/tun2socks/core/stack"
	"github.com/xjasonlyu/tun2socks/proxy"
	"github.com/xjasonlyu/tun2socks/tunnel"
	"net"
	"os"
	"sync"
	"time"
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

func NewTun2socksG(fd int, mtu int, socksPort int, router string, dnsPort int, hijackDns bool, debug bool) (*Tun2socksG, error) {
	file := os.NewFile(uintptr(fd), "")
	if file == nil {
		return nil, errors.New("failed to open TUN file descriptor")
	}

	tunDevice, err := rwbased.New(file, uint32(mtu))
	if err != nil {
		return nil, err
	}
	gvisorStack, err := stack.New(tunDevice, &fakeTunnel{}, stack.WithDefault())
	if debug {
		log.SetLevel(log.DEBUG)
	} else {
		log.SetLevel(log.WARN)
	}

	socks5Proxy, err := proxy.NewSocks5(fmt.Sprintf("127.0.0.1:%d", socksPort), "", "")
	if err != nil {
		return nil, err
	}

	dns := fmt.Sprintf("127.0.0.1:%d", dnsPort)
	dnsAddr, err := net.ResolveUDPAddr("udp", dns)
	if err != nil {
		return nil, err
	}

	tunnel.SetUDPTimeout(5 * 60)

	return &Tun2socksG{
		stack:     gvisorStack,
		device:    tunDevice,
		proxy:     socks5Proxy,
		router:    router,
		dns:       dns,
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
	if metadata.DstIP.String() == t.router || metadata.DstPort == 53 && t.hijackDns {
		pc, err := dialer.ListenPacket("udp", "")
		if err != nil {
			return nil, err
		}
		return &dnsPacketConn{conn: pc, addr: t.dnsAddr, target: metadata.UDPAddr()}, nil
	}
	return t.proxy.DialUDP(metadata)
}

type fakeTunnel struct{}

func (*fakeTunnel) Add(conn core.TCPConn) {
	tunnel.Add(conn)
}
func (*fakeTunnel) AddPacket(packet core.UDPPacket) {
	tunnel.AddPacket(packet)
}

type dnsPacketConn struct {
	conn   net.PacketConn
	addr   *net.UDPAddr
	target net.Addr
}

func (pc *dnsPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	pc.target = addr
	return pc.conn.WriteTo(b, pc.addr)
}

func (pc *dnsPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	n, _, err = pc.conn.ReadFrom(p)
	return n, pc.target, err
}

func (pc *dnsPacketConn) Close() error {
	return pc.conn.Close()
}

func (pc *dnsPacketConn) LocalAddr() net.Addr {
	return pc.conn.LocalAddr()
}

func (pc *dnsPacketConn) SetDeadline(t time.Time) error {
	return pc.conn.SetDeadline(t)
}

func (pc *dnsPacketConn) SetReadDeadline(t time.Time) error {
	return pc.conn.SetReadDeadline(t)
}

func (pc *dnsPacketConn) SetWriteDeadline(t time.Time) error {
	return pc.conn.SetWriteDeadline(t)
}
