package libcore

import (
	"github.com/pkg/errors"
	"github.com/sagernet/gomobile/asset"
	"github.com/v2fly/v2ray-core/v4/common/platform/filesystem"
	"github.com/xjasonlyu/tun2socks/log"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const (
	geoipDat         = "geoip.dat"
	geositeDat       = "geosite.dat"
	browserForwarder = "index.js"
	geoipVersion     = "geoip.version.txt"
	geositeVersion   = "geosite.version.txt"
	coreVersion      = "core.version.txt"
)

var assetsPrefix string
var internalAssetsPath string
var externalAssetsPath string

var extracted map[string]bool
var assetsAccess *sync.Mutex

func InitializeV2Ray(internalAssets string, externalAssets string, prefix string) error {
	assetsAccess = new(sync.Mutex)
	assetsAccess.Lock()
	extracted = make(map[string]bool)

	assetsPrefix = prefix
	internalAssetsPath = internalAssets
	externalAssetsPath = externalAssets

	filesystem.NewFileSeeker = func(path string) (io.ReadSeekCloser, error) {
		_, fileName := filepath.Split(path)

		if !extracted[fileName] {
			assetsAccess.Lock()
			assetsAccess.Unlock()
		}

		paths := []string{
			internalAssetsPath + fileName,
			externalAssetsPath + fileName,
		}

		var err error

		for _, path = range paths {
			_, err = os.Stat(path)
			if err == nil {
				return os.Open(path)
			}
		}

		file, err := asset.Open(assetsPrefix + fileName)
		if err == nil {
			extracted[fileName] = true
			return file, nil
		}

		err = extractAssetName(fileName, false)
		if err != nil {
			return nil, err
		}

		for _, path = range paths {
			_, err = os.Stat(path)
			if err == nil {
				return os.Open(path)
			}
			if !os.IsNotExist(err) {
				return nil, err
			}
		}

		return nil, err
	}

	filesystem.NewFileReader = func(path string) (io.ReadCloser, error) {
		return filesystem.NewFileSeeker(path)
	}

	extract := func(name string) {
		err := extractAssetName(name, false)
		if err != nil {
			log.Warnf("Extract %s failed: %v", geoipDat, err)
		} else {
			extracted[name] = true
		}
	}

	go func() {
		defer assetsAccess.Unlock()

		extract(geoipDat)
		extract(geositeDat)
		extract(browserForwarder)
	}()

	return nil
}

func extractAssetName(name string, force bool) error {
	var dir string
	if name == browserForwarder {
		dir = internalAssetsPath
	} else {
		dir = externalAssetsPath
	}
	var version string
	switch name {
	case geoipDat:
		version = geoipVersion
	case geositeDat:
		version = geositeVersion
	case browserForwarder:
		version = coreVersion
	}

	var localVersion string
	var assetVersion string

	loadAssetVersion := func() error {
		av, err := asset.Open(assetsPrefix + version)
		if err != nil {
			return errors.WithMessage(err, "open version in assets")
		}
		b, err := ioutil.ReadAll(av)
		closeIgnore(av)
		if err != nil {
			return errors.WithMessage(err, "read internal version")
		}
		assetVersion = string(b)
		return nil
	}

	doExtract := false
	// check version
	if _, nf := os.Stat(dir + version); nf != nil {
		doExtract = true
	}
	if !doExtract {
		b, err := ioutil.ReadFile(dir + version)
		if err != nil {
			doExtract = true
			_ = os.RemoveAll(version)
		} else {
			localVersion = string(b)
			err = loadAssetVersion()
			if err != nil {
				return err
			}
			av, err := strconv.ParseUint(assetVersion, 10, 64)
			if err != nil {
				doExtract = assetVersion != localVersion || force
			} else {
				lv, err := strconv.ParseUint(localVersion, 10, 64)
				doExtract = err != nil || av > lv || av == lv && force
			}
		}
	}
	if doExtract {
		if assetVersion == "" {
			err := loadAssetVersion()
			if err != nil {
				return err
			}
		}
	} else {
		return nil
	}

	err := extractAsset(assetsPrefix+name+".xz", dir+name)
	if err == nil {
		err = Unxz(dir + name)

	}
	if err != nil {
		return err
	}

	o, err := os.Create(dir + version)
	if err != nil {
		return err
	}
	_, err = io.WriteString(o, assetVersion)
	closeIgnore(o)
	return err
}

func extractAsset(assetPath string, path string) error {
	i, err := asset.Open(assetPath)
	if err != nil {
		return err
	}
	defer closeIgnore(i)
	o, err := os.Create(path)
	if err != nil {
		return err
	}
	defer closeIgnore(o)
	_, err = io.Copy(o, i)
	return err
}
