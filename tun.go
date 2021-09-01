package libcore

import (
	"context"
	"errors"
	"fmt"
	"github.com/Dreamacro/clash/common/pool"
	"github.com/miekg/dns"
	v2rayCore "github.com/v2fly/v2ray-core/v4"
	v2rayNet "github.com/v2fly/v2ray-core/v4/common/net"
	"github.com/v2fly/v2ray-core/v4/common/session"
	"github.com/v2fly/v2ray-core/v4/common/task"
	"github.com/xjasonlyu/tun2socks/core"
	"github.com/xjasonlyu/tun2socks/core/device/rwbased"
	"github.com/xjasonlyu/tun2socks/core/stack"
	"github.com/xjasonlyu/tun2socks/log"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type Tun2socks struct {
	access    sync.Mutex
	stack     *stack.Stack
	device    *rwbased.Endpoint
	router    string
	hijackDns bool
	v2ray     *V2RayInstance
	udpTable  *natTable
	fakedns   bool
	sniffing  bool
	debug     bool

	dumpUid      bool
	trafficStats bool
	appStats     map[uint16]*appStats
}

var uidDumper UidDumper

type UidInfo struct {
	PackageName string
	Label       string
}

type UidDumper interface {
	DumpUid(ipv6 bool, udp bool, srcIp string, srcPort int32, destIp string, destPort int32) (int32, error)
	GetUidInfo(uid int32) (*UidInfo, error)
}

func SetUidDumper(dumper UidDumper) {
	uidDumper = dumper
}

var foregroundUid uint16

func SetForegroundUid(uid int32) {
	foregroundUid = uint16(uid)
}

var foregroundImeUid uint16

func SetForegroundImeUid(uid int32) {
	foregroundImeUid = uint16(uid)
}

const (
	appStatusForeground = "foreground"
	appStatusBackground = "background"
)

func NewTun2socks(fd int32, mtu int32, v2ray *V2RayInstance, router string, hijackDns bool, sniffing bool, fakedns bool, debug bool, dumpUid bool, trafficStats bool) (*Tun2socks, error) {
	file := os.NewFile(uintptr(fd), "")
	if file == nil {
		return nil, errors.New("failed to open TUN file descriptor")
	}
	tun := &Tun2socks{
		router:       router,
		hijackDns:    hijackDns,
		v2ray:        v2ray,
		udpTable:     &natTable{},
		sniffing:     sniffing,
		fakedns:      fakedns,
		debug:        debug,
		dumpUid:      dumpUid,
		trafficStats: trafficStats,
	}

	if trafficStats {
		tun.appStats = map[uint16]*appStats{}
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

	net.DefaultResolver.Dial = tun.dialDNS
	return tun, nil
}

func (t *Tun2socks) Close() {
	t.access.Lock()
	defer t.access.Unlock()

	net.DefaultResolver.Dial = nil
	t.stack.Close()
}

func (t *Tun2socks) Add(conn core.TCPConn) {
	id := conn.ID()

	la := fmt.Sprintf("tcp:%s", net.JoinHostPort(id.RemoteAddress.String(), strconv.Itoa(int(id.RemotePort))))
	src, err := v2rayNet.ParseDestination(la)
	if err != nil {
		log.Errorf("[TCP] parse source address %s failed: %s", la, err.Error())
		return
	}
	if src.Address.Family().IsDomain() {
		log.Errorf("[TCP] conn with domain src %s received: %s", la, err.Error())
		return
	}
	da := fmt.Sprintf("tcp:%s", net.JoinHostPort(id.LocalAddress.String(), strconv.Itoa(int(id.LocalPort))))
	dest, err := v2rayNet.ParseDestination(da)
	if err != nil {
		log.Errorf("[TCP] parse destination address %s failed: %s", da, err.Error())
		return
	}
	if dest.Address.Family().IsDomain() {
		log.Errorf("[TCP] conn with domain destination %s received: %s", da, err.Error())
		return
	}

	inbound := &session.Inbound{
		Source: src,
		Tag:    "socks",
	}

	isDns := dest.Address.String() == t.router || dest.Port == 53
	if isDns {
		inbound.Tag = "dns-in"
	}

	var uid uint16
	var self bool

	if t.dumpUid || t.trafficStats {
		u, err := uidDumper.DumpUid(dest.Address.Family().IsIPv6(), false, src.Address.IP().String(), int32(src.Port), dest.Address.IP().String(), int32(dest.Port))
		if err == nil {
			uid = uint16(u)
			var info *UidInfo
			self = uid > 0 && int(uid) == os.Getuid()
			if t.debug && !self && uid >= 10000 {
				if err == nil {
					info, _ = uidDumper.GetUidInfo(int32(uid))
				}
				if info == nil {
					log.Infof("[TCP] %s ==> %s", src.NetAddr(), dest.NetAddr())
				} else {
					log.Infof("[TCP][%s (%d/%s)] %s ==> %s", info.Label, uid, info.PackageName, src.NetAddr(), dest.NetAddr())
				}
			}

			if uid < 10000 {
				uid = 1000
			}

			inbound.Uid = uint32(uid)

			if uid == foregroundUid || uid == foregroundImeUid {
				inbound.AppStatus = append(inbound.AppStatus, appStatusForeground)
			} else {
				inbound.AppStatus = append(inbound.AppStatus, appStatusBackground)
			}
		}
	}

	ctx := session.ContextWithInbound(context.Background(), inbound)

	if !isDns && (t.sniffing || t.fakedns) {
		req := session.SniffingRequest{
			Enabled:      true,
			MetadataOnly: t.fakedns && !t.sniffing,
		}
		if t.sniffing && t.fakedns {
			req.OverrideDestinationForProtocol = []string{"fakedns", "http", "tls"}
		}
		if t.sniffing && !t.fakedns {
			req.OverrideDestinationForProtocol = []string{"http", "tls"}
		}
		if !t.sniffing && t.fakedns {
			req.OverrideDestinationForProtocol = []string{"fakedns"}
		}
		ctx = session.ContextWithContent(ctx, &session.Content{
			SniffingRequest: req,
		})
	}

	destConn, err := v2rayCore.Dial(ctx, t.v2ray.core, dest)

	if err != nil {
		log.Errorf("[TCP] dial failed: %s", err.Error())
		return
	}

	if t.trafficStats && !self && !isDns {

		t.access.Lock()
		if !t.trafficStats {
			t.access.Unlock()
		} else {

			stats := t.appStats[uid]
			if stats == nil {
				stats = &appStats{}
				t.appStats[uid] = stats
			}
			t.access.Unlock()
			atomic.AddInt32(&stats.tcpConn, 1)
			atomic.AddUint32(&stats.tcpConnTotal, 1)
			atomic.StoreInt64(&stats.deactivateAt, 0)
			defer func() {
				if atomic.AddInt32(&stats.tcpConn, -1)+atomic.LoadInt32(&stats.udpConn) == 0 {
					atomic.StoreInt64(&stats.deactivateAt, time.Now().Unix())
				}
			}()
			destConn = &statsConn{destConn, &stats.uplink, &stats.downlink}
		}
	}

	_ = task.Run(ctx, func() error {
		_, _ = io.Copy(conn, destConn)
		return io.EOF
	}, func() error {
		_, _ = io.Copy(destConn, conn)
		return io.EOF
	})

	_ = conn.Close()
	_ = destConn.Close()
}

func (t *Tun2socks) AddPacket(packet core.UDPPacket) {
	go t.addPacket(packet)
}

func (t *Tun2socks) addPacket(packet core.UDPPacket) {
	id := packet.ID()
	la := fmt.Sprintf("udp:%s", net.JoinHostPort(id.RemoteAddress.String(), strconv.Itoa(int(id.RemotePort))))
	src, err := v2rayNet.ParseDestination(la)
	if err != nil {
		log.Errorf("[UDP] parse source address %s failed: %s", la, err.Error())
		return
	}
	if src.Address.Family().IsDomain() {
		log.Errorf("[UDP] conn with domain src %s received: %s", la, err.Error())
		return
	}
	da := fmt.Sprintf("udp:%s", net.JoinHostPort(id.LocalAddress.String(), strconv.Itoa(int(id.LocalPort))))
	dest, err := v2rayNet.ParseDestination(da)
	if err != nil {
		log.Errorf("[UDP] parse destination address %s failed: %s", da, err.Error())
		return
	}
	if dest.Address.Family().IsDomain() {
		log.Errorf("[UDP] conn with domain destination %s received: %s", da, err.Error())
		return
	}

	natKey := src.NetAddr()

	sendTo := func(drop bool) bool {
		conn := t.udpTable.Get(natKey)
		if conn == nil {
			return false
		}

		if drop {
			defer packet.Drop()
		}

		_, err := conn.Write(packet.Data())
		if err != nil {
			_ = conn.Close()
		}
		return true
	}

	if sendTo(true) {
		return
	}

	lockKey := natKey + "-lock"
	cond, loaded := t.udpTable.GetOrCreateLock(lockKey)
	if loaded {
		cond.L.Lock()
		cond.Wait()
		sendTo(true)
		cond.L.Unlock()
		return
	}

	t.udpTable.Delete(lockKey)
	cond.Broadcast()

	srcIp := src.Address.IP()
	dstIp := dest.Address.IP()

	inbound := &session.Inbound{
		Source: src,
		Tag:    "socks",
	}
	isDns := dest.Address.String() == t.router

	if !isDns && t.hijackDns {
		dnsMsg := dns.Msg{}
		err := dnsMsg.Unpack(packet.Data())
		if err == nil && !dnsMsg.Response && len(dnsMsg.Question) > 0 {
			isDns = true
		}
	}

	if isDns {
		inbound.Tag = "dns-in"
	}

	var uid uint16
	var self bool

	if t.dumpUid || t.trafficStats {

		u, err := uidDumper.DumpUid(srcIp.To4() == nil, true, srcIp.String(), int32(src.Port), dstIp.String(), int32(dest.Port))
		if err == nil {
			uid = uint16(u)
			var info *UidInfo
			self = uid > 0 && int(uid) == os.Getuid()

			if t.debug && !self && uid >= 1000 {
				if err == nil {
					info, _ = uidDumper.GetUidInfo(int32(uid))
				}
				var tag string
				if !isDns {
					tag = "UDP"
				} else {
					tag = "DNS"
				}

				if info == nil {
					log.Infof("[%s] %s ==> %s", tag, src.NetAddr(), dest.NetAddr())
				} else {
					log.Infof("[%s][%s (%d/%s)] %s ==> %s", tag, info.Label, uid, info.PackageName, src.NetAddr(), dest.NetAddr())
				}
			}

			if uid < 10000 {
				uid = 1000
			}

			inbound.Uid = uint32(uid)
			if uid == foregroundUid || uid == foregroundImeUid {
				inbound.AppStatus = append(inbound.AppStatus, appStatusForeground)
			} else {
				inbound.AppStatus = append(inbound.AppStatus, appStatusBackground)
			}

		}

	}

	ctx := session.ContextWithInbound(context.Background(), inbound)

	if !isDns && t.fakedns {
		ctx = session.ContextWithContent(ctx, &session.Content{
			SniffingRequest: session.SniffingRequest{
				Enabled:                        true,
				MetadataOnly:                   t.fakedns && !t.sniffing,
				OverrideDestinationForProtocol: []string{"fakedns"},
			},
		})
	}

	conn, err := v2rayCore.Dial(ctx, t.v2ray.core, dest)

	if err != nil {
		log.Errorf("[UDP] dial failed: %s", err.Error())
		return
	}

	if t.trafficStats && !self && !isDns {
		t.access.Lock()
		if !t.trafficStats {
			t.access.Unlock()
		} else {
			stats := t.appStats[uid]
			if stats == nil {
				stats = &appStats{}
				t.appStats[uid] = stats
			}
			t.access.Unlock()
			atomic.AddInt32(&stats.udpConn, 1)
			atomic.AddUint32(&stats.udpConnTotal, 1)
			atomic.StoreInt64(&stats.deactivateAt, 0)
			defer func() {
				if atomic.AddInt32(&stats.udpConn, -1)+atomic.LoadInt32(&stats.tcpConn) == 0 {
					atomic.StoreInt64(&stats.deactivateAt, time.Now().Unix())
				}
			}()
			conn = &statsConn{conn, &stats.uplink, &stats.downlink}
		}
	}

	t.udpTable.Set(natKey, conn)

	go sendTo(false)

	buf := pool.Get(pool.RelayBufferSize)

	for {
		n, err := conn.Read(buf)
		if err != nil {
			break
		}
		_, err = packet.WriteBack(buf[:n], nil)
		if err != nil {
			break
		}
	}

	// close

	_ = pool.Put(buf)
	_ = conn.Close()
	packet.Drop()
	t.udpTable.Delete(natKey)
}

func (t *Tun2socks) dialDNS(ctx context.Context, _, _ string) (net.Conn, error) {
	conn, err := v2rayCore.Dial(session.ContextWithInbound(ctx, &session.Inbound{
		Tag: "dns-in",
	}), t.v2ray.core, v2rayNet.Destination{
		Network: v2rayNet.Network_UDP,
		Address: v2rayNet.ParseAddress("1.0.0.1"),
		Port:    53,
	})
	if err != nil {
		return nil, err
	}
	return &wrappedConn{conn}, nil
}

type wrappedConn struct {
	net.Conn
}

func (c wrappedConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	n, err = c.Conn.Read(p)
	if err == nil {
		addr = c.Conn.RemoteAddr()
	}
	return
}

func (c wrappedConn) WriteTo(p []byte, _ net.Addr) (n int, err error) {
	return c.Conn.Write(p)
}

type natTable struct {
	mapping sync.Map
}

func (t *natTable) Set(key string, pc net.Conn) {
	t.mapping.Store(key, pc)
}

func (t *natTable) Get(key string) net.Conn {
	item, exist := t.mapping.Load(key)
	if !exist {
		return nil
	}
	return item.(net.Conn)
}

func (t *natTable) GetOrCreateLock(key string) (*sync.Cond, bool) {
	item, loaded := t.mapping.LoadOrStore(key, sync.NewCond(&sync.Mutex{}))
	return item.(*sync.Cond), loaded
}

func (t *natTable) Delete(key string) {
	t.mapping.Delete(key)
}
