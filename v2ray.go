package libsagernet

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"

	core "github.com/v2fly/v2ray-core/v4"
	appLog "github.com/v2fly/v2ray-core/v4/app/log"
	commonLog "github.com/v2fly/v2ray-core/v4/common/log"
	"github.com/v2fly/v2ray-core/v4/common/platform/filesystem"
	"github.com/v2fly/v2ray-core/v4/features/stats"
	"github.com/v2fly/v2ray-core/v4/infra/conf/serial"
	_ "github.com/v2fly/v2ray-core/v4/main/distro/all"
)

func GetV2RayVersion() string {
	return core.Version()
}

func InitializeV2Ray(assetsPath string, assetsPrefix string, memReader bool) error {

	const envName = "core.location.asset"
	err := os.Setenv(envName, assetsPath)
	if err != nil {
		return err
	}

	filesystem.NewFileReader = func(path string) (io.ReadCloser, error) {
		return openAssets(assetsPrefix, path, memReader)
	}
	filesystem.NewFileSeeker = func(path string) (io.ReadSeekCloser, error) {
		return openAssets(assetsPrefix, path, memReader)
	}

	return nil
}

type stdoutLogWriter struct {
	logger *log.Logger
}

func (w *stdoutLogWriter) Write(s string) error {
	w.logger.Print(s)
	return nil
}

func (w *stdoutLogWriter) Close() error {
	return nil
}

func init() {
	_ = appLog.RegisterHandlerCreator(appLog.LogType_Console,
		func(lt appLog.LogType,
			options appLog.HandlerCreatorOptions) (commonLog.Handler, error) {
			logger := log.New(os.Stdout, "", 0)
			return commonLog.NewLogger(func() commonLog.Writer {
				return &stdoutLogWriter{
					logger: logger,
				}
			}), nil
		})
}

type V2RayInstance struct {
	sync.Mutex

	started      bool
	core         *core.Instance
	statsManager stats.Manager
}

func NewV2rayInstance() V2RayInstance {
	return V2RayInstance{}
}

func (instance *V2RayInstance) LoadConfig(content string) error {
	instance.Lock()
	defer instance.Unlock()

	config, err := serial.LoadJSONConfig(strings.NewReader(content))
	runtime.GC()
	if err != nil {
		return err
	}
	c, err := core.New(config)
	if err != nil {
		return err
	}
	instance.core = c
	instance.statsManager = c.GetFeature(stats.ManagerType()).(stats.Manager)
	return nil
}

func (instance *V2RayInstance) Start() error {
	instance.Lock()
	defer instance.Unlock()

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
	instance.Lock()
	defer instance.Unlock()

	if instance.started {
		return instance.core.Close()
	}
	return nil
}
