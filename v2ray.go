package libcore

import (
	"errors"
	"fmt"
	core "github.com/v2fly/v2ray-core/v4"
	"github.com/v2fly/v2ray-core/v4/features/extension"
	"github.com/v2fly/v2ray-core/v4/features/stats"
	"github.com/v2fly/v2ray-core/v4/infra/conf/serial"
	_ "github.com/v2fly/v2ray-core/v4/main/distro/all"
	"strings"
	"sync"
)

func GetV2RayVersion() string {
	return core.Version()
}

type V2RayInstance struct {
	access       sync.Mutex
	started      bool
	core         *core.Instance
	statsManager stats.Manager
	observatory  extension.Observatory
}

func NewV2rayInstance() *V2RayInstance {
	return &V2RayInstance{}
}

func (instance *V2RayInstance) LoadConfig(content string, forTest bool) error {
	instance.access.Lock()
	defer instance.access.Unlock()
	config, err := serial.LoadJSONConfig(strings.NewReader(content))
	if err != nil {
		if strings.HasSuffix(err.Error(), "not found in geoip.dat") || strings.HasSuffix(err.Error(), "geoip.dat: no such file or directory") {
			err = extractAssetName(geoipDat, true)
			if err != nil {
				return err
			}
		} else if strings.HasSuffix(err.Error(), "not found in geosite.dat") || strings.HasSuffix(err.Error(), "geosite.dat: no such file or directory") {
			err = extractAssetName(geositeDat, true)
			if err != nil {
				return err
			}
		} else {
			return err
		}

		config, err = serial.LoadJSONConfig(strings.NewReader(content))
		if err != nil {
			return err
		}
		return err
	}
	if forTest {
		config.Inbound = nil
		config.App = config.App[:4]
	}
	c, err := core.New(config)
	if err != nil {
		return err
	}
	instance.core = c
	instance.statsManager = c.GetFeature(stats.ManagerType()).(stats.Manager)
	observatory := c.GetFeature(extension.ObservatoryType())
	if observatory != nil {
		instance.observatory = observatory.(extension.Observatory)
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
