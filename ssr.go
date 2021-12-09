package libcore

import (
	"bytes"
	"flag"
	"strconv"

	"github.com/Dreamacro/clash/transport/ssr/obfs"
	"github.com/Dreamacro/clash/transport/ssr/protocol"
	"github.com/v2fly/v2ray-core/v4/common/buf"
	"github.com/v2fly/v2ray-core/v4/proxy/shadowsocks"
	"github.com/v2fly/v2ray-core/v4/transport/internet"
)

var (
	_ shadowsocks.SIP003Plugin   = (*shadowsocksrPlugin)(nil)
	_ shadowsocks.StreamPlugin   = (*shadowsocksrPlugin)(nil)
	_ shadowsocks.ProtocolPlugin = (*shadowsocksrPlugin)(nil)
)

func init() {
	shadowsocks.RegisterPlugin("shadowsocksr", func() shadowsocks.SIP003Plugin {
		return new(shadowsocksrPlugin)
	})
}

type shadowsocksrPlugin struct {
	host          string
	port          int
	obfs          string
	obfsParam     string
	protocol      string
	protocolParam string

	o obfs.Obfs
	p protocol.Protocol
}

func (p *shadowsocksrPlugin) Init(_ string, _ string, remoteHost string, remotePort string, _ string, pluginArgs []string, account *shadowsocks.MemoryAccount) error {
	fs := flag.NewFlagSet("shadowsocksr", flag.ContinueOnError)
	fs.StringVar(&p.obfs, "obfs", "origin", "")
	fs.StringVar(&p.obfsParam, "obfs-param", "", "")
	fs.StringVar(&p.protocol, "protocol", "origin", "")
	fs.StringVar(&p.protocolParam, "protocol-param", "", "")
	if err := fs.Parse(pluginArgs); err != nil {
		return newError("shadowsocksr: failed to parse args").Base(err)
	}
	p.host = remoteHost
	p.port, _ = strconv.Atoi(remotePort)

	obfs, obfsOverhead, err := obfs.PickObfs(p.obfs, &obfs.Base{
		Host:   p.host,
		Port:   p.port,
		Key:    account.Key,
		IVSize: int(account.Cipher.IVSize()),
		Param:  p.obfsParam,
	})
	if err != nil {
		return newError("failed to create ssr obfs").Base(err)
	}

	protocol, err := protocol.PickProtocol(p.protocol, &protocol.Base{
		Key:      account.Key,
		Overhead: obfsOverhead,
		Param:    p.protocolParam,
	})
	if err != nil {
		return newError("failed to create ssr protocol").Base(err)
	}

	p.o = obfs
	p.p = protocol

	return nil
}

func (p *shadowsocksrPlugin) Close() error {
	return nil
}

func (p *shadowsocksrPlugin) StreamConn(conn internet.Connection) internet.Connection {
	return p.o.StreamConn(conn)
}

func (p *shadowsocksrPlugin) StreamReader(reader buf.Reader, iv []byte) (buf.Reader, error) {
	conn := p.p.StreamConn(buf.NewConnection(buf.ConnectionOutputMulti(reader)), iv)
	return buf.NewReader(conn), nil
}

func (p *shadowsocksrPlugin) StreamWriter(writer buf.Writer, iv []byte) (buf.Writer, error) {
	conn := p.p.StreamConn(buf.NewConnection(buf.ConnectionInputMulti(writer)), iv)
	return buf.NewWriter(conn), nil
}

func (p *shadowsocksrPlugin) EncodePacket(buffer *buf.Buffer) (*buf.Buffer, error) {
	packet := &bytes.Buffer{}
	err := p.p.EncodePacket(packet, buffer.Bytes())
	buffer.Release()
	if err != nil {
		buffer.Release()
		return nil, err
	}
	return buf.FromBytes(packet.Bytes()), nil
}

func (p *shadowsocksrPlugin) DecodePacket(buffer *buf.Buffer) (*buf.Buffer, error) {
	packet, err := p.p.DecodePacket(buffer.Bytes())
	buffer.Release()
	if err != nil {
		return nil, err
	}
	return buf.FromBytes(packet), nil
}
