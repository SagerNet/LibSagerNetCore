package libcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Dreamacro/clash/common/pool"
	"github.com/miekg/dns"
	v2rayCore "github.com/v2fly/v2ray-core/v4"
	v2rayNet "github.com/v2fly/v2ray-core/v4/common/net"
	"github.com/v2fly/v2ray-core/v4/common/session"
	"github.com/xjasonlyu/tun2socks/core"
	"github.com/xjasonlyu/tun2socks/core/device/rwbased"
	"github.com/xjasonlyu/tun2socks/core/stack"
	"github.com/xjasonlyu/tun2socks/log"
	"io"
	"net"
	"os"
	"strconv"
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
	udpTable  *natTable
	debug     bool
	dumpUid   bool
}

var uidDumper UidDumper

type UidInfo struct {
	PackageName string
	Label       string
}

type UidDumper interface {
	DumpUid(ipv6 bool, udp bool, srcIp string, srcPort int, destIp string, destPort int) (int, error)
	GetUidInfo(uid int) (*UidInfo, error)
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
		udpTable:  &natTable{},
		debug:     debug,
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

	var uidRules = map[int]int{}
	err = json.Unmarshal([]byte(uidRule), &uidRules)
	tun.dumpUid = len(uidRules) > 0
	tun.uidRule = uidRules

	return tun, nil
}

func (t *Tun2socks) Close() {
	t.access.Lock()
	defer t.access.Unlock()

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

	srcIp := src.Address.IP()
	dstIp := dest.Address.IP()

	inbound := ""
	isDns := dest.Address.String() == t.router
	if isDns {
		inbound = "dns-in"
	}

	if t.dumpUid || t.debug {

		uid, err := uidDumper.DumpUid(srcIp.To4() == nil, false, srcIp.String(), int(src.Port), dstIp.String(), int(dest.Port))
		var info *UidInfo

		if err == nil {
			rule := t.uidRule[uid]
			if rule != 0 && !isDns {
				inbound = fmt.Sprint("uid-", uid)
			}
		}

		if t.debug {
			if err == nil {
				info, _ = uidDumper.GetUidInfo(uid)
			}
			if info == nil {
				log.Infof("[TCP] %s ==> %s", src.NetAddr(), dest.NetAddr())
			} else {
				log.Infof("[TCP][%s (%d/%s)] %s ==> %s", info.Label, uid, info.PackageName, src.NetAddr(), dest.NetAddr())
			}

		}

	}

	ctx := context.Background()

	if inbound != "" {
		ctx = session.ContextWithInbound(ctx, &session.Inbound{
			Source: src,
			Tag:    inbound,
		})
	}

	destConn, err := v2rayCore.Dial(ctx, t.v2ray.core, dest)
	if err != nil {
		log.Errorf("[TCP] dial failed: %s", err.Error())
		return
	}

	go func() {
		_, _ = io.Copy(destConn, conn)
	}()
	_, _ = io.Copy(conn, destConn)

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

	inbound := ""
	isDns := dest.Address.String() == t.router || dest.Port == 53 && t.hijackDns

	if !isDns && t.hijackDns {
		dnsMsg := dns.Msg{}
		err := dnsMsg.Unpack(packet.Data())
		if err == nil {
			if !dnsMsg.Response && len(dnsMsg.Question) > 0 {
				isDns = true
			}
		}
	}

	if isDns {
		inbound = "dns-in"
	}

	if t.dumpUid || t.debug {

		uid, err := uidDumper.DumpUid(srcIp.To4() == nil, true, srcIp.String(), int(src.Port), dstIp.String(), int(dest.Port))
		var info *UidInfo

		if err == nil {
			rule := t.uidRule[uid]
			if rule != 0 && !isDns {
				inbound = fmt.Sprint("uid-", uid)
			}
		}

		if t.debug {
			if err == nil {
				info, _ = uidDumper.GetUidInfo(uid)
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
	}

	ctx := context.Background()

	if inbound != "" {
		ctx = session.ContextWithInbound(ctx, &session.Inbound{
			Source: src,
			Tag:    inbound,
		})
	}

	conn, err := v2rayCore.Dial(ctx, t.v2ray.core, dest)
	if err != nil {
		log.Errorf("[UDP] dial failed: %s", err.Error())
		return
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
