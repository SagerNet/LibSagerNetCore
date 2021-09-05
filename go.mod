module libcore

go 1.16

require (
	github.com/Dreamacro/clash v1.6.5
	github.com/golang/protobuf v1.5.2
	github.com/miekg/dns v1.1.43
	github.com/pkg/errors v0.9.1
	github.com/sagernet/gomobile v0.0.0-20210905032500-701a995ff844
	github.com/sagernet/libping v0.1.1
	github.com/sagernet/sagerconnect v0.1.7
	github.com/sirupsen/logrus v1.8.1
	github.com/ulikunitz/xz v0.5.10
	github.com/v2fly/v2ray-core/v4 v4.42.1
	github.com/xjasonlyu/tun2socks v1.18.4-0.20210821024837-525d424fca52
	golang.org/x/crypto v0.0.0-20210817164053-32db794688a5
	golang.org/x/sys v0.0.0-20210823070655-63515b42dcdf

)

replace github.com/v2fly/v2ray-core/v4 v4.42.1 => github.com/sagernet/v2ray-core/v4 v4.42.2-0.20210905032340-2639850443bc
