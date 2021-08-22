package libcore

import (
	"bytes"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/sagernet/gomobile/asset"
	"github.com/ulikunitz/xz"
)

type memReader struct {
	file   io.ReadSeekCloser
	reader io.ReadSeeker
}

func (asset memReader) Read(p []byte) (n int, err error) {
	return asset.reader.Read(p)
}

func (asset memReader) Seek(offset int64, whence int) (int64, error) {
	return asset.reader.Seek(offset, whence)
}

func (asset memReader) Close() error {
	return asset.file.Close()
}

func newMemReader(file io.ReadSeekCloser) (io.ReadSeekCloser, error) {
	reader, err := xz.NewReader(file)
	if err != nil {
		return nil, err
	}
	byteArray, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	bytesReader := bytes.NewReader(byteArray)
	return memReader{
		file:   file,
		reader: bytesReader,
	}, nil
}

type xzReader struct {
	file   io.ReadSeekCloser
	reader *xz.Reader
	index  int64
}

func newXzReader(file io.ReadSeekCloser) (io.ReadSeekCloser, error) {
	reader, err := xz.NewReader(file)
	if err != nil {
		return nil, err
	}
	return &xzReader{
		index:  0,
		file:   file,
		reader: reader,
	}, nil
}

func (asset *xzReader) Read(p []byte) (n int, err error) {
	n, err = asset.reader.Read(p)
	if err != nil {
		log.Printf("xzReader read %d failed: %v", n, err)
	} else {
		asset.index += int64(n)
	}
	return
}

func (asset *xzReader) Seek(offset int64, _ int) (int64, error) {
	if offset < 0 {
		// recreate reader
		_, err := asset.file.Seek(0, io.SeekStart)
		if err != nil {
			log.Printf("asset seek failed: %v", err)
			return 0, err
		}
		reader, err := xz.NewReader(asset.file)
		if err != nil {
			log.Printf("recreate xz reader failed: %v", err)
			return 0, err
		}
		asset.reader = reader
		offset = asset.index + offset
		asset.index = offset
	} else {
		asset.index += offset
	}

	return io.CopyN(io.Discard, asset.reader, offset)
}

func (asset *xzReader) Close() error {
	return asset.file.Close()
}

func openAssets(assetsPrefix string, path string, memReader bool) (io.ReadSeekCloser, error) {
	_, fileName := filepath.Split(path)
	path = geoAssetsPath + fileName

	_, notExistsInFileSystemError := os.Stat(path)
	if notExistsInFileSystemError == nil {
		log.Printf("load geo asset %s", path)
		return os.Open(path)
	}
	if !os.IsNotExist(notExistsInFileSystemError) {
		return nil, notExistsInFileSystemError
	}
	log.Printf("%s not found", path)

	xzPath := path + ".xz"
	_, notExistsInFileSystemError = os.Stat(xzPath)
	if notExistsInFileSystemError == nil {
		file, err := os.Open(xzPath)
		if err != nil {
			return nil, err
		}
		log.Printf("load geo asset %s", xzPath)
		if memReader {
			return newMemReader(file)
		} else {
			return newXzReader(file)
		}
	}
	if !os.IsNotExist(notExistsInFileSystemError) {
		return nil, notExistsInFileSystemError
	}

	path = assetsPrefix + fileName

	assetFile, err := asset.Open(path)
	if err == nil {
		log.Printf("load geo asset %s", path)
		return assetFile, nil
	}

	path += ".xz"
	assetFile, err = asset.Open(path)
	if err != nil {
		return nil, err
	}
	log.Printf("load geo asset %s", path)
	if memReader {
		return newMemReader(assetFile)
	} else {
		return newXzReader(assetFile)
	}
}
