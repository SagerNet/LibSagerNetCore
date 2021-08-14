package libsagernet

import (
	"errors"
	"sync"

	"github.com/p4gefau1t/trojan-go/proxy"
	_ "github.com/p4gefau1t/trojan-go/proxy/client"
)

func GetTrojanVersion() string {
	return "0.10.4"
}

type TrojanInstance struct {
	sync.Mutex

	core    *proxy.Proxy
	started bool
}

func NewTrojanInstance() TrojanInstance {
	return TrojanInstance{}
}

func (instance *TrojanInstance) LoadConfig(content string, isJSON bool) error {
	instance.Lock()
	defer instance.Unlock()

	core, err := proxy.NewProxyFromConfigData([]byte(content), isJSON)
	if err != nil {
		return err
	}
	instance.core = core
	return nil
}

func (instance *TrojanInstance) Start() error {
	instance.Lock()
	defer instance.Unlock()

	if instance.started {
		return errors.New("already started")
	}
	//goland:noinspection GoUnhandledErrorResult
	go instance.core.Run()
	instance.started = true
	return nil
}

func (instance *TrojanInstance) Close() error {
	instance.Lock()
	defer instance.Unlock()

	if instance.started {
		return instance.core.Close()
	}
	return nil
}
