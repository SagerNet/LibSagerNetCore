package libsagernet

import (
	"context"
	"errors"
	"fmt"
	"github.com/v2fly/v2ray-core/v4/common/net"
	"github.com/v2fly/v2ray-core/v4/transport/internet"
	"golang.org/x/sys/unix"
	"os"
	"time"
)

type Protector interface {
	Protect(fd int) bool
}

func SetProtector(protector Protector) {
	internet.UseAlternativeSystemDialer(ProtectedDialer{
		protector: protector,
		resolver:  &net.Resolver{PreferGo: false},
	})
}

type ProtectedDialer struct {
	protector Protector
	resolver  *net.Resolver
}

func (dialer ProtectedDialer) Dial(ctx context.Context, source net.Address, destination net.Destination, sockopt *internet.SocketConfig) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	host, port, err := net.SplitHostPort(destination.NetAddr())
	if err != nil {
		return nil, err
	}

	portNum, err := dialer.resolver.LookupPort(ctx, "tcp", port)
	if err != nil {
		return nil, err
	}

	addresses, err := dialer.resolver.LookupIPAddr(ctx, host)
	if err == nil && len(addresses) == 0 {
		err = errors.New("NXDOMAIN")
	}
	if err != nil {
		return nil, err
	}

	fd, err := getFd(destination.Network)
	if err != nil {
		return nil, err
	}

	if !dialer.protector.Protect(fd) {
		return nil, errors.New("protect failed")
	}

	socketAddress := &unix.SockaddrInet6{
		Port: portNum,
	}
	copy(socketAddress.Addr[:], addresses[0].IP)

	err = unix.Connect(fd, socketAddress)
	if err != nil {
		return nil, err
	}

	file := os.NewFile(uintptr(fd), "Socket")
	if file == nil {
		return nil, errors.New("failed to connect to fd")
	}

	defer safeClose(file)

	conn, err := net.FileConn(file)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

func getFd(network net.Network) (fd int, err error) {
	switch network {
	case net.Network_TCP:
		fd, err = unix.Socket(unix.AF_INET6, unix.SOCK_STREAM, unix.IPPROTO_TCP)
	case net.Network_UDP:
		fd, err = unix.Socket(unix.AF_INET6, unix.SOCK_DGRAM, unix.IPPROTO_UDP)
	case net.Network_UNIX:
		fd, err = unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	default:
		err = fmt.Errorf("unknow network")
	}
	return
}
