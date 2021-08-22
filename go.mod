module libcore

go 1.16

require (
	github.com/Dreamacro/clash v1.6.5
	github.com/pkg/errors v0.9.1
	github.com/sagernet/gomobile v0.0.0-20210822074701-68a55075c7d2
	github.com/sagernet/libping v0.1.0
	github.com/sagernet/sagerconnect v0.1.7
	github.com/sirupsen/logrus v1.8.1
	github.com/ulikunitz/xz v0.5.10
	github.com/v2fly/v2ray-core/v4 v4.41.1
	github.com/xjasonlyu/tun2socks v1.18.4-0.20210813034434-85cf694b8fed
	golang.org/x/sys v0.0.0-20210820121016-41cdb8703e55
)

replace github.com/Dreamacro/clash v1.6.5 => github.com/ClashDotNetFramework/experimental-clash v1.7.2

replace github.com/xjasonlyu/tun2socks v0.0.0 => github.com/sagernet/tun2socks v1.18.4-0.20210822134916-352a06d579ac
