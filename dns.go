package libcore

import (
	"github.com/miekg/dns"
	"github.com/xjasonlyu/tun2socks/log"
	"net"
	"time"
)

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
