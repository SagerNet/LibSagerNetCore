package libcore

import (
	"github.com/ulikunitz/xz"
	"io"
	"os"
)

func Unxz(path string) error {
	i, err := os.Open(path)
	if err != nil {
		return err
	}
	r, err := xz.NewReader(i)
	if err != nil {
		closeIgnore(i)
		return err
	}
	o, err := os.Create(path + ".tmp")
	if err != nil {
		closeIgnore(i)
		return err
	}
	_, err = io.Copy(o, r)
	closeIgnore(i)
	closeIgnore(o)
	if err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
