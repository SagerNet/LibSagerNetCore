package lwip

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/v2fly/v2ray-core/v4/common/bytespool"
	v2rayNet "github.com/v2fly/v2ray-core/v4/common/net"
	"libcore/lwip/core"
	"libcore/tun"
	"net"
	"os"
	"sync"
)

var _ tun.Tun = (*Lwip)(nil)

type Lwip struct {
	running bool
	pool    *sync.Pool

	Dev     *os.File
	Stack   core.LWIPStack
	Handler tun.Handler
}

func New(dev *os.File, mtu int32, handler tun.Handler) (*Lwip, error) {
	t := &Lwip{
		running: true,
		pool:    bytespool.GetPool(mtu),

		Dev:     dev,
		Stack:   core.NewLWIPStack(),
		Handler: handler,
	}
	core.RegisterOutputFn(dev.Write)
	core.RegisterTCPConnHandler(t)
	core.RegisterUDPConnHandler(t)
	core.SetMtu(mtu)

	go t.processPacket()
	return t, nil
}

func (l *Lwip) processPacket() {
	if !l.running {
		return
	}
	defer l.processPacket()
	buffer := l.pool.Get().([]byte)
	defer l.pool.Put(buffer)

	length, err := l.Dev.Read(buffer)
	if err != nil {
		logrus.Warnf("failed to read packet from TUN: %v", err)
		return
	}
	if length == 0 {
		logrus.Info("read EOF from TUN")
		l.running = false
		return
	}
	_, err = l.Stack.Write(buffer)
	if err != nil {
		logrus.Warnf("failed to write packet to LWIP: %v", err)
		l.running = false
		return
	}

}

func (l *Lwip) Handle(conn net.Conn) error {
	src, _ := v2rayNet.ParseDestination(fmt.Sprint("tcp:", conn.LocalAddr().String()))
	dst, _ := v2rayNet.ParseDestination(fmt.Sprint("tcp:", conn.RemoteAddr().String()))
	go l.Handler.NewConnection(src, dst, conn)
	return nil
}

func (l *Lwip) ReceiveTo(conn core.UDPConn, data []byte, addr *net.UDPAddr) error {
	src, _ := v2rayNet.ParseDestination(fmt.Sprint("udp:", conn.LocalAddr().String()))
	dst, _ := v2rayNet.ParseDestination(fmt.Sprint("udp:", addr.String()))
	go l.Handler.NewPacket(src, dst, data, func(bytes []byte, from *net.UDPAddr) (int, error) {
		if from == nil {
			from = addr
		}
		return conn.WriteFrom(bytes, from)
	}, conn)
	return nil
}

func (l *Lwip) Close() error {
	l.running = false
	return l.Stack.Close()
}
