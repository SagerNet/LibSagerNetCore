package libsagernet

import (
	"io"
	"os"
	"path/filepath"

	"github.com/ulikunitz/xz"
	"golang.org/x/mobile/asset"
)

type splitReader struct {
	closer io.Closer
	reader io.Reader
}

func (asset splitReader) Read(p []byte) (n int, err error) {
	return asset.reader.Read(p)
}

func (asset splitReader) Seek(offset int64, _ int) (int64, error) {
	if offset == 0 {
		return 0, nil
	}
	return io.CopyN(io.Discard, asset.reader, offset)
}

func (asset splitReader) Close() error {
	return asset.closer.Close()
}

func openAssets(assetsPrefix string, path string) (io.ReadSeekCloser, error) {
	_, notExistsInFileSystemError := os.Stat(path)
	if notExistsInFileSystemError == nil {
		return os.Open(path)
	}
	if !os.IsNotExist(notExistsInFileSystemError) {
		return nil, notExistsInFileSystemError
	}
	_, notExistsInFileSystemError = os.Stat(path + ".xz")
	if notExistsInFileSystemError == nil {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		reader, err := xz.NewReader(file)
		if err != nil {
			return nil, err
		}
		return splitReader{reader: reader, closer: file}, nil
	}
	if !os.IsNotExist(notExistsInFileSystemError) {
		return nil, notExistsInFileSystemError
	}

	_, fileName := filepath.Split(path)

	assetFile, err := asset.Open(assetsPrefix + fileName)
	if err == nil {
		return assetFile, nil
	}

	assetFile, err = asset.Open(assetsPrefix + fileName + ".xz")
	if err != nil {
		return nil, err
	}

	reader, err := xz.NewReader(assetFile)
	if err != nil {
		return nil, err
	}

	return splitReader{reader: reader, closer: assetFile}, nil

}
