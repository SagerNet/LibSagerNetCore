package libcore

import (
	"context"
	"encoding/json"
	"github.com/golang/protobuf/proto"
	"github.com/v2fly/v2ray-core/v4"
	"github.com/v2fly/v2ray-core/v4/common/serial"
	"github.com/v2fly/v2ray-core/v4/infra/conf/cfgcommon"
	"github.com/v2fly/v2ray-core/v4/infra/conf/geodata"
	syntheticDns "github.com/v2fly/v2ray-core/v4/infra/conf/synthetic/dns"
	syntheticRouter "github.com/v2fly/v2ray-core/v4/infra/conf/synthetic/router"
	"net"
	"runtime"
)

type V2RayBuilder struct {
	config *core.Config
	ctx    context.Context
}

func NewV2RayBuilder(content []byte) (*V2RayBuilder, error) {
	c := new(core.Config)
	err := proto.Unmarshal(content, c)
	if err != nil {
		return nil, err
	}
	loader, err := geodata.GetGeoDataLoader("memconservative")
	if err != nil {
		return nil, err
	}
	ctx := cfgcommon.NewConfigureLoadingContext(context.Background())
	cfgcommon.SetGeoDataLoader(ctx, loader)
	return &V2RayBuilder{c, ctx}, nil
}

func ParseIP(s string) []byte {
	return net.ParseIP(s)
}

func (b *V2RayBuilder) SetRouter(content string) error {
	sc := new(syntheticRouter.RouterConfig)
	err := json.Unmarshal([]byte(content), sc)
	if err != nil {
		return err
	}
	rc, err := sc.BuildV5(b.ctx)
	if err != nil {
		return err
	}
	b.config.App = append(b.config.App, serial.ToTypedMessage(rc))
	return nil
}

func (b *V2RayBuilder) SetDNS(content string) error {
	sc := &syntheticDns.DNSConfig{}
	err := json.Unmarshal([]byte(content), sc)
	if err != nil {
		return err
	}
	dc, err := sc.BuildV5(b.ctx)
	if err != nil {
		return err
	}
	b.config.App = append(b.config.App, serial.ToTypedMessage(dc))
	return nil
}

func (b *V2RayBuilder) Close() error {
	b.ctx = nil
	runtime.GC()
	return nil
}
