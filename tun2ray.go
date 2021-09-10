package libcore

import (
	"context"
	"errors"
	"github.com/Dreamacro/clash/common/pool"
	"github.com/miekg/dns"
	"github.com/sirupsen/logrus"
	v2rayCore "github.com/v2fly/v2ray-core/v4"
	v2rayNet "github.com/v2fly/v2ray-core/v4/common/net"
	"github.com/v2fly/v2ray-core/v4/common/session"
	"github.com/v2fly/v2ray-core/v4/common/task"
	"io"
	"libcore/gvisor"
	"libcore/lwip"
	"libcore/tun"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var _ tun.Handler = (*Tun2ray)(nil)

type Tun2ray struct {
	access    sync.Mutex
	dev       tun.Tun
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

const (
	appStatusForeground = "foreground"
	appStatusBackground = "background"
)

func NewTun2socks(fd int32, mtu int32, v2ray *V2RayInstance, router string, gVisor bool, hijackDns bool, sniffing bool, fakedns bool, debug bool, dumpUid bool, trafficStats bool) (*Tun2ray, error) {
	file := os.NewFile(uintptr(fd), "")
	if file == nil {
		return nil, errors.New("failed to open TUN file descriptor")
	}
	if debug {
		logrus.SetLevel(logrus.DebugLevel)
	} else {
		logrus.SetLevel(logrus.WarnLevel)
	}
	t := &Tun2ray{
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
		t.appStats = map[uint16]*appStats{}
	}
	var dev tun.Tun
	var err error

	if gVisor {
		dev, err = gvisor.New(fd, mtu, gvisor.DefaultNIC, t)
	} else {
		dev, err = lwip.New(fd, mtu, t)
	}
	if err != nil {
		return nil, err
	}
	t.dev = dev

	net.DefaultResolver.Dial = t.dialDNS
	return t, nil
}

func (t *Tun2ray) Close() {
	t.access.Lock()
	defer t.access.Unlock()

	net.DefaultResolver.Dial = nil
	closeIgnore(t.dev)
}

func (t *Tun2ray) NewConnection(source v2rayNet.Destination, destination v2rayNet.Destination, conn net.Conn) {
	inbound := &session.Inbound{
		Source: source,
		Tag:    "socks",
	}

	isDns := destination.Address.String() == t.router || destination.Port == 53
	if isDns {
		inbound.Tag = "dns-in"
	}

	var uid uint16
	var self bool

	if t.dumpUid || t.trafficStats {
		u, err := uidDumper.DumpUid(destination.Address.Family().IsIPv6(), false, source.Address.IP().String(), int32(source.Port), destination.Address.IP().String(), int32(destination.Port))
		if err == nil {
			uid = uint16(u)
			var info *UidInfo
			self = uid > 0 && int(uid) == os.Getuid()
			if t.debug && !self && uid >= 10000 {
				if err == nil {
					info, _ = uidDumper.GetUidInfo(int32(uid))
				}
				if info == nil {
					logrus.Infof("[TCP] %s ==> %s", source.NetAddr(), destination.NetAddr())
				} else {
					logrus.Infof("[TCP][%s (%d/%s)] %s ==> %s", info.Label, uid, info.PackageName, source.NetAddr(), destination.NetAddr())
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

	destConn, err := v2rayCore.Dial(ctx, t.v2ray.core, destination)

	if err != nil {
		logrus.Errorf("[TCP] dial failed: %s", err.Error())
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

func (t *Tun2ray) NewPacket(source v2rayNet.Destination, destination v2rayNet.Destination, data []byte, writeBack func([]byte, *net.UDPAddr) (int, error)) {
	natKey := source.NetAddr()

	sendTo := func() bool {
		conn := t.udpTable.Get(natKey)
		if conn == nil {
			return false
		}
		_, err := conn.Write(data)
		if err != nil {
			_ = conn.Close()
		}
		return true
	}

	if sendTo() {
		return
	}

	lockKey := natKey + "-lock"
	cond, loaded := t.udpTable.GetOrCreateLock(lockKey)
	if loaded {
		cond.L.Lock()
		cond.Wait()
		sendTo()
		cond.L.Unlock()
		return
	}

	t.udpTable.Delete(lockKey)
	cond.Broadcast()

	inbound := &session.Inbound{
		Source: source,
		Tag:    "socks",
	}
	isDns := destination.Address.String() == t.router

	if !isDns && t.hijackDns {
		dnsMsg := dns.Msg{}
		err := dnsMsg.Unpack(data)
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

		u, err := uidDumper.DumpUid(source.Address.Family().IsIPv6(), true, source.Address.String(), int32(source.Port), destination.Address.String(), int32(destination.Port))
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
					logrus.Infof("[%s] %s ==> %s", tag, source.NetAddr(), destination.NetAddr())
				} else {
					logrus.Infof("[%s][%s (%d/%s)] %s ==> %s", tag, info.Label, uid, info.PackageName, source.NetAddr(), destination.NetAddr())
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

	conn, err := v2rayCore.Dial(ctx, t.v2ray.core, destination)

	if err != nil {
		logrus.Errorf("[UDP] dial failed: %s", err.Error())
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

	go sendTo()

	buf := pool.Get(pool.RelayBufferSize)

	for {
		n, err := conn.Read(buf)
		if err != nil {
			break
		}
		_, err = writeBack(buf[:n], nil)
		if err != nil {
			break
		}
	}

	// close

	_ = pool.Put(buf)
	_ = conn.Close()
	t.udpTable.Delete(natKey)
}

func (t *Tun2ray) dialDNS(ctx context.Context, _, _ string) (net.Conn, error) {
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
