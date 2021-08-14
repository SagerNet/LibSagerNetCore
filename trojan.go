package libsagernet

import (
	"errors"
	"github.com/p4gefau1t/trojan-go/proxy"
	_ "github.com/p4gefau1t/trojan-go/proxy/client"
	"sync"
)

func GetTrojanVersion() string {
	return "0.10.4"
}

type TrojanInstance struct {
	core    *proxy.Proxy
	access  sync.Mutex
	started bool
}

func NewTrojanInstance() TrojanInstance {
	return TrojanInstance{}
}

func (instance *TrojanInstance) LoadConfig(content string, isJSON bool) error {
	instance.access.Lock()
	defer instance.access.Unlock()
	core, err := proxy.NewProxyFromConfigData([]byte(content), isJSON)
	if err != nil {
		return err
	}
	instance.core = core
	return nil
}

func (instance *TrojanInstance) Start() error {
	instance.access.Lock()
	defer instance.access.Unlock()
	if instance.started {
		return errors.New("already started")
	}
	//goland:noinspection GoUnhandledErrorResult
	go instance.core.Run()
	instance.started = true
	return nil
}

func (instance *TrojanInstance) Close() error {
	instance.access.Lock()
	defer instance.access.Unlock()
	if instance.started {
		return instance.core.Close()
	}
	return nil
}
