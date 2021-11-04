package libcore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/v2fly/v2ray-core/v4"
	"github.com/v2fly/v2ray-core/v4/common"
	"github.com/v2fly/v2ray-core/v4/common/buf"
	"github.com/v2fly/v2ray-core/v4/common/net"
	"github.com/v2fly/v2ray-core/v4/common/protocol/udp"
	"github.com/v2fly/v2ray-core/v4/common/signal"
	"github.com/v2fly/v2ray-core/v4/features"
	"github.com/v2fly/v2ray-core/v4/features/dns"
	"github.com/v2fly/v2ray-core/v4/features/extension"
	"github.com/v2fly/v2ray-core/v4/features/routing"
	"github.com/v2fly/v2ray-core/v4/features/stats"
	"github.com/v2fly/v2ray-core/v4/infra/conf/serial"
	_ "github.com/v2fly/v2ray-core/v4/main/distro/all"
	"github.com/v2fly/v2ray-core/v4/transport"
)

func GetV2RayVersion() string {
	return core.Version() + "-sn-2"
}

type V2RayInstance struct {
	access       sync.Mutex
	started      bool
	core         *core.Instance
	statsManager stats.Manager
	observatory  features.TaggedFeatures
	dispatcher   routing.Dispatcher
	dnsClient    dns.Client
}

func NewV2rayInstance() *V2RayInstance {
	return &V2RayInstance{}
}

func (instance *V2RayInstance) LoadConfig(content string) error {
	instance.access.Lock()
	defer instance.access.Unlock()
	config, err := serial.LoadJSONConfig(strings.NewReader(content))
	if err != nil {
		if strings.HasSuffix(err.Error(), "geoip.dat: no such file or directory") {
			err = extractAssetName(geoipDat, true)
		} else if strings.HasSuffix(err.Error(), "not found in geoip.dat") {
			err = extractAssetName(geoipDat, false)
		} else if strings.HasSuffix(err.Error(), "geosite.dat: no such file or directory") {
			err = extractAssetName(geositeDat, true)
		} else if strings.HasSuffix(err.Error(), "not found in geosite.dat") {
			err = extractAssetName(geositeDat, false)
		}
		if err == nil {
			config, err = serial.LoadJSONConfig(strings.NewReader(content))
		}
	}
	if err != nil {
		return err
	}
	c, err := core.New(config)
	if err != nil {
		return err
	}
	instance.core = c
	instance.statsManager = c.GetFeature(stats.ManagerType()).(stats.Manager)
	instance.dispatcher = c.GetFeature(routing.DispatcherType()).(routing.Dispatcher)
	instance.dnsClient = c.GetFeature(dns.ClientType()).(dns.Client)

	o := c.GetFeature(extension.ObservatoryType())
	if o != nil {
		instance.observatory = o.(features.TaggedFeatures)
	}
	return nil
}

func (instance *V2RayInstance) Start() error {
	instance.access.Lock()
	defer instance.access.Unlock()
	if instance.started {
		return errors.New("already started")
	}
	if instance.core == nil {
		return errors.New("not initialized")
	}
	err := instance.core.Start()
	if err != nil {
		return err
	}
	instance.started = true
	return nil
}

func (instance *V2RayInstance) QueryStats(tag string, direct string) int64 {
	if instance.statsManager == nil {
		return 0
	}
	counter := instance.statsManager.GetCounter(fmt.Sprintf("outbound>>>%s>>>traffic>>>%s", tag, direct))
	if counter == nil {
		return 0
	}
	return counter.Set(0)
}

func (instance *V2RayInstance) Close() error {
	instance.access.Lock()
	defer instance.access.Unlock()
	if instance.started {
		return instance.core.Close()
	}
	return nil
}

func (instance *V2RayInstance) dialContext(ctx context.Context, destination net.Destination) (net.Conn, error) {
	ctx = core.WithContext(ctx, instance.core)
	r, err := instance.dispatcher.Dispatch(ctx, destination)
	if err != nil {
		return nil, err
	}
	var readerOpt buf.ConnectionOption
	if destination.Network == net.Network_TCP {
		readerOpt = buf.ConnectionOutputMulti(r.Reader)
	} else {
		readerOpt = buf.ConnectionOutputMultiUDP(r.Reader)
	}
	return buf.NewConnection(buf.ConnectionInputMulti(r.Writer), readerOpt), nil
}

func (instance *V2RayInstance) dialUDP(ctx context.Context, destination net.Destination, timeout time.Duration) (packetConn, error) {
	ctx, cancel := context.WithCancel(core.WithContext(ctx, instance.core))
	link, err := instance.dispatcher.Dispatch(ctx, destination)
	if err != nil {
		cancel()
		return nil, err
	}
	c := &dispatcherConn{
		dest:   destination,
		link:   link,
		ctx:    ctx,
		cancel: cancel,
		cache:  make(chan *udp.Packet, 16),
	}
	c.timer = signal.CancelAfterInactivity(ctx, func() {
		closeIgnore(c)
	}, timeout)
	go c.handleInput()
	return c, nil
}

var _ packetConn = (*dispatcherConn)(nil)

type dispatcherConn struct {
	dest  net.Destination
	link  *transport.Link
	timer *signal.ActivityTimer

	ctx    context.Context
	cancel context.CancelFunc

	cache chan *udp.Packet
}

func (c *dispatcherConn) handleInput() {
	defer closeIgnore(c)
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		mb, err := c.link.Reader.ReadMultiBuffer()
		if err != nil {
			buf.ReleaseMulti(mb)
			return
		}
		for _, buffer := range mb {
			packet := udp.Packet{
				Payload: buffer,
			}
			if buffer.Endpoint == nil {
				packet.Source = c.dest
			} else {
				packet.Source = *buffer.Endpoint
			}
			select {
			case c.cache <- &packet:
				continue
			case <-c.ctx.Done():
			default:
			}
			buffer.Release()
		}
	}
}

func (c *dispatcherConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	select {
	case <-c.ctx.Done():
		return 0, nil, io.EOF
	case packet := <-c.cache:
		n := copy(p, packet.Payload.Bytes())
		return n, &net.UDPAddr{
			IP:   packet.Source.Address.IP(),
			Port: int(packet.Source.Port),
		}, nil
	}
}

func (c *dispatcherConn) readFrom() (p []byte, addr net.Addr, err error) {
	select {
	case <-c.ctx.Done():
		return nil, nil, io.EOF
	case packet := <-c.cache:
		return packet.Payload.Bytes(), &net.UDPAddr{
			IP:   packet.Source.Address.IP(),
			Port: int(packet.Source.Port),
		}, nil
	}
}

func (c *dispatcherConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	buffer := buf.New()
	raw := buffer.Extend(buf.Size)
	n = copy(raw, p)
	buffer.Resize(0, int32(n))

	endpoint := net.DestinationFromAddr(addr)
	buffer.Endpoint = &endpoint

	err = c.link.Writer.WriteMultiBuffer(buf.MultiBuffer{buffer})
	return
}

func (c *dispatcherConn) LocalAddr() net.Addr {
	return &net.UDPAddr{
		IP:   []byte{0, 0, 0, 0},
		Port: 0,
	}
}

func (c *dispatcherConn) Close() error {
	select {
	case <-c.ctx.Done():
		return nil
	default:
	}

	c.cancel()
	_ = common.Interrupt(c.link.Reader)
	_ = common.Interrupt(c.link.Writer)
	close(c.cache)

	return nil
}

func (c *dispatcherConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *dispatcherConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *dispatcherConn) SetWriteDeadline(t time.Time) error {
	return nil
}
