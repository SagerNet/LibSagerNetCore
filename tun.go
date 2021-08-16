package libcore

import (
	"errors"
	"fmt"
	"github.com/SagerNet/go-tun2socks/core"
	"github.com/SagerNet/go-tun2socks/log"
	"github.com/SagerNet/go-tun2socks/log/simple"
	"github.com/SagerNet/go-tun2socks/proxy/redirect"
	"github.com/SagerNet/go-tun2socks/proxy/socks"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"sync"
	"time"
)

func init() {
	log.RegisterLogger(simple.NewSimpleLogger())
}

type Tun2socks struct {
	access          sync.Mutex
	tun             *os.File
	mtu             int
	router          string
	dns             string
	hijackDns       bool
	socksTcpHandler core.TCPConnHandler
	dnsTcpHandler   core.TCPConnHandler
	socksUdpHandler core.UDPConnHandler
	dnsUdpHandler   core.UDPConnHandler
	lwip            core.LWIPStack
	running         bool
	debug           bool
}

func NewTun2socks(fd int, mtu int, socksPort int, router string, dnsPort int, hijackDns bool, debug bool) (*Tun2socks, error) {
	if fd < 0 {
		return nil, errors.New("must provide a valid TUN file descriptor")
	}
	// Make a copy of `fd` so that os.File's finalizer doesn't close `fd`.
	newFd, err := unix.Dup(fd)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(newFd), "")
	if file == nil {
		return nil, errors.New("failed to open TUN file descriptor")
	}

	dns := fmt.Sprintf("127.0.0.1:%d", dnsPort)
	log.Infof("dns: %s", dns)
	return &Tun2socks{
		tun:             file,
		mtu:             mtu,
		router:          router,
		dns:             dns,
		hijackDns:       hijackDns,
		lwip:            core.NewLWIPStack(),
		socksTcpHandler: socks.NewTCPHandler("127.0.0.1", uint16(socksPort)),
		dnsTcpHandler:   redirect.NewTCPHandler(dns),
		socksUdpHandler: socks.NewUDPHandler("127.0.0.1", uint16(socksPort), 5*time.Minute),
		dnsUdpHandler:   redirect.NewUDPHandler(dns, 1*time.Minute),
		debug:           debug,
	}, nil
}

func (t *Tun2socks) Start() error {
	t.access.Lock()
	defer t.access.Unlock()

	if t.running {
		return errors.New("already started")
	}

	core.RegisterOutputFn(t.tun.Write)
	core.RegisterTCPConnHandler(t)
	core.RegisterUDPConnHandler(t)

	var logLevel log.LogLevel
	if t.debug {
		logLevel = log.DEBUG
	} else {
		logLevel = log.WARN
	}
	log.SetLevel(logLevel)

	t.running = true
	go t.processPackets()

	return nil
}

func (t *Tun2socks) processPackets() {
	buffer := make([]byte, t.mtu)
	for t.running {
		length, err := t.tun.Read(buffer)
		if err != nil {
			log.Warnf("failed to read packet from TUN: %v", err)
			continue
		}
		if length == 0 {
			log.Infof("read EOF from TUN")
			continue
		}
		_, err = t.lwip.Write(buffer)
		if err != nil {
			log.Warnf("failed to write packet to LWIP: %v", err)
		}
	}
}

func (t *Tun2socks) Close() {
	t.access.Lock()
	defer t.access.Unlock()

	t.running = false
	_ = t.lwip.Close()
	_ = t.tun.Close()
}

func (t *Tun2socks) Handle(conn net.Conn, target *net.TCPAddr) error {
	if target.IP.String() == t.router || target.Port == 53 && t.hijackDns {
		return t.dnsTcpHandler.Handle(conn, target)
	} else {
		return t.socksTcpHandler.Handle(conn, target)
	}
}

func (t *Tun2socks) Connect(conn core.UDPConn, target *net.UDPAddr) error {
	if target.IP.String() == t.router || target.Port == 53 && t.hijackDns {
		return t.dnsUdpHandler.Connect(conn, target)
	} else {
		return t.socksUdpHandler.Connect(conn, target)
	}
}

func (t *Tun2socks) ReceiveTo(conn core.UDPConn, data []byte, addr *net.UDPAddr) error {
	if addr.IP.String() == t.router || addr.Port == 53 && t.hijackDns {
		return t.dnsUdpHandler.ReceiveTo(conn, data, addr)
	} else {
		return t.socksUdpHandler.ReceiveTo(conn, data, addr)
	}
}
