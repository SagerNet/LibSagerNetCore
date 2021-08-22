package libcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	v2rayCore "github.com/v2fly/v2ray-core/v4"
	v2rayNet "github.com/v2fly/v2ray-core/v4/common/net"
	"github.com/v2fly/v2ray-core/v4/common/session"
	"github.com/v2fly/v2ray-core/v4/transport/internet"
	"github.com/xjasonlyu/tun2socks/constant"
	"github.com/xjasonlyu/tun2socks/core"
	"github.com/xjasonlyu/tun2socks/core/device/rwbased"
	"github.com/xjasonlyu/tun2socks/core/stack"
	"github.com/xjasonlyu/tun2socks/log"
	"github.com/xjasonlyu/tun2socks/proxy"
	"github.com/xjasonlyu/tun2socks/tunnel"
	"io"
	"net"
	"os"
	"sync"
)

type Tun2socks struct {
	access    sync.Mutex
	stack     *stack.Stack
	device    *rwbased.Endpoint
	router    string
	hijackDns bool
	uidRule   map[int]int
	v2ray     *V2RayInstance
}

var uidDumper UidDumper

type UidDumper interface {
	DumpUid(ipv6 bool, udp bool, srcIp string, srcPort int, destIp string, destPort int) (int, error)
}

func SetUidDumper(dumper UidDumper) {
	uidDumper = dumper
}

func NewTun2socks(fd int, mtu int, v2ray *V2RayInstance, router string, hijackDns bool, debug bool, uidRule string) (*Tun2socks, error) {
	file := os.NewFile(uintptr(fd), "")
	if file == nil {
		return nil, errors.New("failed to open TUN file descriptor")
	}
	tun := &Tun2socks{
		router:    router,
		hijackDns: hijackDns,
		v2ray:     v2ray,
	}

	d, err := rwbased.New(file, uint32(mtu))
	if err != nil {
		return nil, err
	}
	tun.device = d

	s, err := stack.New(d, tun, stack.WithDefault())
	tun.stack = s

	if debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.WarnLevel)
	}

	tunnel.SetUDPTimeout(5 * 60)

	var uidRules = map[int]int{}
	err = json.Unmarshal([]byte(uidRule), &uidRules)

	tun.uidRule = uidRules

	return tun, nil
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

func (t *Tun2socks) Add(conn core.TCPConn) {
	la := fmt.Sprintf("%s:%s", conn.RemoteAddr().Network(), conn.RemoteAddr().String())
	src, err := v2rayNet.ParseDestination(la)
	if err != nil {
		log.Errorf("[TCP] parse source address %s failed: %s", la, err.Error())
		return
	}
	if src.Address.Family().IsDomain() {
		log.Errorf("[TCP] conn with domain src %s received: %s", la, err.Error())
		return
	}
	da := fmt.Sprintf("%s:%s", conn.LocalAddr().Network(), conn.LocalAddr().String())
	dest, err := v2rayNet.ParseDestination(da)
	if err != nil {
		log.Errorf("[TCP] parse destination address %s failed: %s", da, err.Error())
		return
	}
	if dest.Address.Family().IsDomain() {
		log.Errorf("[TCP] conn with domain destination %s received: %s", da, err.Error())
		return
	}

	srcIp := src.Address.IP()
	dstIp := dest.Address.IP()

	log.Infof("[TCP] %s ==> %s", conn.RemoteAddr().String(), conn.LocalAddr().String())

	outbound := ""
	isDns := dest.Address.String() == t.router
	if isDns {
		outbound = "dns-out"
	}
	uid, err := uidDumper.DumpUid(srcIp.To4() == nil, dest.Network == v2rayNet.Network_UDP, srcIp.String(), int(src.Port), dstIp.String(), int(dest.Port))

	if err != nil {
		log.Warnf("[TCP] dumpUid failed: %v", err)
	} else {
		rule := t.uidRule[uid]
		if rule != 0 && !isDns {
			outbound = fmt.Sprint("uid-", uid)
		}
	}

	ctx := context.Background()

	if outbound != "" {
		ctx = session.SetForcedOutboundTagToContext(ctx, outbound)
	}

	destConn, err := v2rayCore.Dial(ctx, t.v2ray.core, dest)
	if err != nil {
		log.Errorf("[TCP] dial failed: %s", err.Error())
	}

	go func() {
		_, _ = io.Copy(destConn, conn)
	}()
	_, _ = io.Copy(conn, destConn)

	_ = conn.Close()
	_ = destConn.Close()
}

func (*Tun2socks) AddPacket(packet core.UDPPacket) {
	tunnel.AddPacket(packet)
}

func (t *Tun2socks) DialContext(context.Context, *constant.Metadata) (net.Conn, error) {
	panic("unexpected")
}

func (t *Tun2socks) DialUDP(metadata *constant.Metadata) (net.PacketConn, error) {
	ctx := context.Background()

	dest := v2rayNet.Destination{
		Network: v2rayNet.Network_UDP,
		Address: v2rayNet.ParseAddress(metadata.DstIP.String()),
		Port:    v2rayNet.Port(metadata.DstPort),
	}

	outbound := ""

	isDns := metadata.DstIP.String() == t.router || metadata.DstPort == 53 && t.hijackDns
	if isDns {
		outbound = "dns-out"
	}

	uid, err := uidDumper.DumpUid(len(metadata.DstIP) == net.IPv6len, true, metadata.SrcIP.String(), int(metadata.SrcPort), metadata.DstIP.String(), int(metadata.DstPort))
	if err != nil {
		log.Warnf("dumpUid failed: %v", err)
	} else {
		rule := t.uidRule[uid]
		if rule == 1 {
			return nil, fmt.Errorf("blocked uid %d", uid)
		} else if rule == 1 && !isDns {
			conn, err := internet.DialSystem(ctx, dest, nil)
			if err != nil {
				return nil, err
			}
			return udpPacketConn{conn}, nil
		} else if rule > 1 && !isDns {
			outbound = fmt.Sprint("uid-", uid)
		}
	}

	if outbound != "" {
		ctx = session.SetForcedOutboundTagToContext(ctx, outbound)
	}

	conn, err := v2rayCore.Dial(ctx, t.v2ray.core, dest)
	if err != nil {
		return nil, err
	}
	return udpPacketConn{Conn: conn}, nil
}

type udpPacketConn struct {
	net.Conn
}

func (u udpPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	n, err = u.Read(p)
	if err != nil {
		return 0, nil, err
	}
	addr = u.RemoteAddr()
	return
}

func (u udpPacketConn) WriteTo(p []byte, _ net.Addr) (n int, err error) {
	return u.Write(p)
}
