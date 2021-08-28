module libcore

go 1.16

require (
	github.com/Dreamacro/clash v1.6.5
	github.com/miekg/dns v1.1.43
	github.com/pkg/errors v0.9.1
	github.com/sagernet/gomobile v0.0.0-20210822074701-68a55075c7d2
	github.com/sagernet/libping v0.1.0
	github.com/sagernet/sagerconnect v0.1.7
	github.com/sirupsen/logrus v1.8.1
	github.com/ulikunitz/xz v0.5.10
	github.com/v2fly/v2ray-core/v4 v4.41.1
	github.com/xjasonlyu/tun2socks v1.18.4-0.20210821024837-525d424fca52
	golang.org/x/sys v0.0.0-20210823070655-63515b42dcdf
)

replace github.com/v2fly/v2ray-core/v4 v4.41.1 => github.com/sagernet/v2ray-core/v4 v4.41.2-0.20210828154311-5f8a46ac56ff

replace github.com/Dreamacro/clash v1.6.5 => github.com/ClashDotNetFramework/experimental-clash v1.7.2
