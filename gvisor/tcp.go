package gvisor

import (
	"fmt"
	"net"
	"strconv"

	v2rayNet "github.com/v2fly/v2ray-core/v4/common/net"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/waiter"
	"libcore/tun"
)

func gTcpHandler(s *stack.Stack, handler tun.Handler) {
	forwarder := tcp.NewForwarder(s, 0, 2<<10, func(request *tcp.ForwarderRequest) {
		id := request.ID()
		waitQueue := new(waiter.Queue)
		endpoint, errT := request.CreateEndpoint(waitQueue)
		if errT != nil {
			newError("failed to create TCP connection").Base(tcpipErr(errT)).WriteToLog()
			// prevent potential half-open TCP connection leak.
			request.Complete(true)
			return
		}
		request.Complete(false)
		srcAddr := net.JoinHostPort(id.RemoteAddress.String(), strconv.Itoa(int(id.RemotePort)))
		src, err := v2rayNet.ParseDestination(fmt.Sprint("tcp:", srcAddr))
		if err != nil {
			newError("[TCP] parse source address ", srcAddr, " failed: ", err).AtWarning().WriteToLog()
			return
		}
		dstAddr := net.JoinHostPort(id.LocalAddress.String(), strconv.Itoa(int(id.LocalPort)))
		dst, err := v2rayNet.ParseDestination(fmt.Sprint("tcp:", dstAddr))
		if err != nil {
			newError("[TCP] parse destination address ", dstAddr, " failed: ", err).AtWarning().WriteToLog()
			return
		}
		go handler.NewConnection(src, dst, gonet.NewTCPConn(waitQueue, endpoint))
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, forwarder.HandlePacket)
}
