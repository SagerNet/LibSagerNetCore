package libcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/miekg/dns"
	v2rayNet "github.com/v2fly/v2ray-core/v4/common/net"
	"github.com/v2fly/v2ray-core/v4/transport/internet"
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
	"time"
)

type Tun2socks struct {
	access     sync.Mutex
	stack      *stack.Stack
	proxy      *proxy.Socks5
	device     *rwbased.Endpoint
	router     string
	dns        string
	dnsAddr    *net.UDPAddr
	hijackDns  bool
	uidRule    map[int]int
	uidProxies map[int]*proxy.Socks5
}

var uidDumper UidDumper

type UidDumper interface {
	DumpUid(ipv6 bool, udp bool, srcIp string, srcPort int, destIp string, destPort int) (int, error)
}

func SetUidDumper(dumper UidDumper) {
	uidDumper = dumper
}

type proxyTunnel struct{}

func (*proxyTunnel) Add(conn core.TCPConn) {
	tunnel.Add(conn)
}
func (*proxyTunnel) AddPacket(packet core.UDPPacket) {
	tunnel.AddPacket(packet)
}

func NewTun2socks(fd int, mtu int, socksPort int, router string, dnsPort int, hijackDns bool, debug bool, uidRule string) (*Tun2socks, error) {
	file := os.NewFile(uintptr(fd), "")
	if file == nil {
		return nil, errors.New("failed to open TUN file descriptor")
	}

	tunDevice, err := rwbased.New(file, uint32(mtu))
	if err != nil {
		return nil, err
	}
	gvisor, err := stack.New(tunDevice, &proxyTunnel{}, stack.WithDefault())

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

	var uidRules = map[int]int{}
	var uidProxies = map[int]*proxy.Socks5{}
	err = json.Unmarshal([]byte(uidRule), &uidRules)
	if err != nil {
		return nil, err
	}
	for uid, rule := range uidRules {
		if rule > 1 {
			uidProxy, err := proxy.NewSocks5(fmt.Sprintf("127.0.0.1:%d", rule), "", "")
			if err != nil {
				return nil, err
			}
			uidProxies[uid] = uidProxy
		}
	}

	return &Tun2socks{
		stack:      gvisor,
		device:     tunDevice,
		proxy:      socks5Proxy,
		router:     router,
		dns:        dnsAddrStr,
		dnsAddr:    dnsAddr,
		hijackDns:  hijackDns,
		uidRule:    uidRules,
		uidProxies: uidProxies,
	}, nil
}

func (t *Tun2socks) Start() {
	t.access.Lock()
	defer t.access.Unlock()

	proxy.SetDialer(t)
}

func (t *Tun2socks) Close() {
	t.access.Lock()
	defer t.access.Unlock()

	t.stack.Close()
}

func (t *Tun2socks) DialContext(ctx context.Context, metadata *constant.Metadata) (net.Conn, error) {
	uid, err := uidDumper.DumpUid(len(metadata.DstIP) == net.IPv6len, false, metadata.SrcIP.String(), int(metadata.SrcPort), metadata.DstIP.String(), int(metadata.DstPort))
	isDns := metadata.DstIP.String() == t.router || metadata.DstPort == 53 && t.hijackDns
	var destProxy *proxy.Socks5
	if err != nil {
		log.Warnf("dumpUid failed: %v", err)
	} else {
		rule := t.uidRule[uid]
		if rule == 1 {
			return nil, errors.New(fmt.Sprintf("blocked uid %d", uid))
		} else if rule == 1 && !isDns {
			// direct
			dest := v2rayNet.Destination{
				Network: v2rayNet.Network_UDP,
				Address: v2rayNet.ParseAddress(metadata.DestinationAddress()),
				Port:    v2rayNet.Port(metadata.DstPort),
			}

			conn, err := internet.DialSystem(ctx, dest, nil)
			if err != nil {
				return nil, err
			}
			return conn, nil
		} else if rule > 1 && !isDns {
			destProxy = t.uidProxies[uid]
		}
	}
	if isDns {
		return dialer.DialContext(ctx, "tcp", t.dns)
	}
	if destProxy == nil {
		destProxy = t.proxy
	}
	return destProxy.DialContext(ctx, metadata)
}

func (t *Tun2socks) DialUDP(metadata *constant.Metadata) (net.PacketConn, error) {
	uid, err := uidDumper.DumpUid(len(metadata.DstIP) == net.IPv6len, true, metadata.SrcIP.String(), int(metadata.SrcPort), metadata.DstIP.String(), int(metadata.DstPort))
	isDns := metadata.DstIP.String() == t.router || metadata.DstPort == 53 && t.hijackDns
	var destProxy *proxy.Socks5
	if err != nil {
		log.Warnf("dumpUid failed: %v", err)
	} else {
		rule := t.uidRule[uid]
		if rule == 1 {
			return nil, errors.New(fmt.Sprintf("blocked uid %d", uid))
		} else if rule == 1 && !isDns {
			// direct
			dest := v2rayNet.Destination{
				Network: v2rayNet.Network_UDP,
				Address: v2rayNet.ParseAddress(metadata.DestinationAddress()),
				Port:    v2rayNet.Port(metadata.DstPort),
			}

			conn, err := internet.DialSystem(context.Background(), dest, nil)
			if err != nil {
				return nil, err
			}

			return udpFileConn{Conn: conn}, nil
		} else if rule > 1 && !isDns {
			destProxy = t.uidProxies[uid]
		}
	}
	if destProxy == nil {
		destProxy = t.proxy
	}

	if isDns {
		return t.newDnsPacketConn(metadata)
	} else {
		return destProxy.DialUDP(metadata)
	}
}

func (t *Tun2socks) newDnsPacketConn(metadata *constant.Metadata) (conn net.PacketConn, err error) {
	conn, err = dialer.ListenPacket("udp", "")
	if err == nil {
		conn = &dnsPacketConn{conn: conn, dnsAddr: t.dnsAddr, realAddr: metadata.UDPAddr()}
	}
	return
}

type dnsPacketConn struct {
	conn     net.PacketConn
	notDns   bool
	dnsAddr  *net.UDPAddr
	realAddr net.Addr
}

func (pc *dnsPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	pc.realAddr = addr
	if !pc.notDns {
		req := new(dns.Msg)
		err := req.Unpack(b)
		if err == nil && !req.Response {
			if len(req.Question) > 0 {
				log.Debugf("new dns query: %s", req.Question[0].Name)
			}
			return pc.conn.WriteTo(b, pc.dnsAddr)
		} else {
			pc.notDns = true
		}
	}
	return pc.conn.WriteTo(b, addr)
}

func (pc *dnsPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	n, realAddr, err := pc.conn.ReadFrom(p)
	if pc.realAddr != nil {
		return n, pc.realAddr, err
	} else {
		return n, realAddr, err
	}
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

type udpFileConn struct {
	net.Conn
}

func (u udpFileConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	n, err = u.Read(p)
	if err != nil {
		return 0, nil, err
	}
	addr = u.RemoteAddr()
	return
}

func (u udpFileConn) WriteTo(p []byte, _ net.Addr) (n int, err error) {
	return u.Write(p)
}
