package gvisor

import (
	"github.com/sirupsen/logrus"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/sniffer"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"libcore/tun"
	"os"
)

var _ tun.Tun = (*GVisor)(nil)

type GVisor struct {
	Endpoint stack.LinkEndpoint
	PcapFile *os.File
	Stack    *stack.Stack
}

func (t *GVisor) Close() error {
	t.Stack.Close()
	if t.PcapFile != nil {
		_ = t.PcapFile.Close()
	}
	return nil
}

const DefaultNIC tcpip.NICID = 0x01

func New(dev int32, mtu int32, handler tun.Handler, nicId tcpip.NICID, pcap bool, pcapFile *os.File, snapLen uint32) (*GVisor, error) {
	var endpoint stack.LinkEndpoint
	endpoint, _ = newRwEndpoint(dev, mtu)
	if pcap {
		pcap, err := sniffer.NewWithWriter(endpoint, pcapFile, snapLen)
		if err != nil {
			return nil, err
		}
		endpoint = pcap
	}
	s := stack.New(stack.Options{
		NetworkProtocols: []stack.NetworkProtocolFactory{
			ipv4.NewProtocol,
			ipv6.NewProtocol,
		},
		TransportProtocols: []stack.TransportProtocolFactory{
			tcp.NewProtocol,
			udp.NewProtocol,
			icmp.NewProtocol4,
			icmp.NewProtocol6,
		},
	})
	s.SetRouteTable([]tcpip.Route{
		{
			Destination: header.IPv4EmptySubnet,
			NIC:         nicId,
		},
		{
			Destination: header.IPv6EmptySubnet,
			NIC:         nicId,
		},
	})
	gTcpHandler(s, handler)
	gUdpHandler(s, handler)
	gMust(s.CreateNIC(nicId, endpoint))
	gMust(s.SetSpoofing(nicId, true))
	gMust(s.SetPromiscuousMode(nicId, true))

	return &GVisor{endpoint, pcapFile, s}, nil
}

func gMust(err tcpip.Error) {
	if err != nil {
		logrus.Panicln(err.String())
	}
}
