package gvisor

import (
	"fmt"
	v2rayNet "github.com/v2fly/v2ray-core/v4/common/net"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/waiter"
	"libcore/tun"
	"net"
	"strconv"
)

func gTcpHandler(s *stack.Stack, handler tun.Handler) {
	forwarder := tcp.NewForwarder(s, 0, 2<<10, func(request *tcp.ForwarderRequest) {
		id := request.ID()
		waitQueue := new(waiter.Queue)
		endpoint, err := request.CreateEndpoint(waitQueue)
		if err != nil {
			// prevent potential half-open TCP connection leak.
			request.Complete(true)
			return
		}
		request.Complete(false)
		src, _ := v2rayNet.ParseDestination(fmt.Sprint("tcp:", net.JoinHostPort(id.RemoteAddress.String(), strconv.Itoa(int(id.RemotePort)))))
		dst, _ := v2rayNet.ParseDestination(fmt.Sprint("tcp:", net.JoinHostPort(id.LocalAddress.String(), strconv.Itoa(int(id.LocalPort)))))
		go handler.NewConnection(src, dst, gonet.NewTCPConn(waitQueue, endpoint))
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, forwarder.HandlePacket)
}
