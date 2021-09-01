package libcore

import (
	"context"
	"errors"
	"fmt"
	v2rayNet "github.com/v2fly/v2ray-core/v4/common/net"
	"github.com/v2fly/v2ray-core/v4/features/dns"
	"github.com/v2fly/v2ray-core/v4/transport/internet"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"time"
)

type Protector interface {
	Protect(fd int32) bool
}

func SetProtector(protector Protector) {
	internet.UseAlternativeSystemDialer(protectedDialer{
		protector: protector,
		resolver:  net.DefaultResolver,
	})
	internet.UseAlternativeSystemDNSDialer(protectedDialer{
		protector: protector,
		resolver:  &net.Resolver{PreferGo: false},
	})
}

type protectedDialer struct {
	protector Protector
	resolver  *net.Resolver
}

func (dialer protectedDialer) Dial(ctx context.Context, source v2rayNet.Address, destination v2rayNet.Destination, sockopt *internet.SocketConfig) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var destIp net.IP
	if destination.Address.Family().IsIP() {
		destIp = destination.Address.IP()
	} else {
		addresses, err := dialer.resolver.LookupIPAddr(ctx, destination.Address.Domain())
		if err == nil && len(addresses) == 0 {
			err = dns.ErrEmptyResponse
		}
		if err != nil {
			return nil, err
		}

		if ipv6Mode == 3 {
			// ipv6 only

			for _, addr := range addresses {
				v2Addr := v2rayNet.ParseAddress(addr.String())
				if v2Addr.Family().IsIPv6() {
					destIp = addr.IP
					break
				}
			}
		}
		if destIp == nil {
			destIp = addresses[0].IP
		}
	}

	ipv6 := len(destIp) != net.IPv4len
	fd, err := getFd(destination.Network, ipv6)
	if err != nil {
		return nil, err
	}

	if !dialer.protector.Protect(int32(fd)) {
		return nil, errors.New("protect failed")
	}

	var sockaddr unix.Sockaddr
	if !ipv6 {
		socketAddress := &unix.SockaddrInet4{
			Port: int(destination.Port),
		}
		copy(socketAddress.Addr[:], destIp)
		sockaddr = socketAddress
	} else {
		socketAddress := &unix.SockaddrInet6{
			Port: int(destination.Port),
		}
		copy(socketAddress.Addr[:], destIp)
		sockaddr = socketAddress
	}

	err = unix.Connect(fd, sockaddr)
	if err != nil {
		return nil, err
	}

	file := os.NewFile(uintptr(fd), "socket")
	if file == nil {
		return nil, errors.New("failed to connect to fd")
	}

	conn, err := net.FileConn(file)
	if err != nil {
		return nil, err
	}

	_ = file.Close()
	return conn, nil
}

func getFd(network v2rayNet.Network, ipv6 bool) (fd int, err error) {
	var af int
	if !ipv6 {
		af = unix.AF_INET
	} else {
		af = unix.AF_INET6
	}
	switch network {
	case v2rayNet.Network_TCP:
		fd, err = unix.Socket(af, unix.SOCK_STREAM, unix.IPPROTO_TCP)
	case v2rayNet.Network_UDP:
		fd, err = unix.Socket(af, unix.SOCK_DGRAM, unix.IPPROTO_UDP)
	case v2rayNet.Network_UNIX:
		fd, err = unix.Socket(af, unix.SOCK_STREAM, 0)
	default:
		err = fmt.Errorf("unknow network")
	}
	return
}
