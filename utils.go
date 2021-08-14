package libsagernet

import (
	"github.com/v2fly/v2ray-core/v4/common"
	"golang.org/x/sys/unix"
	"log"
)

func closeFd(fd int) {
	err := unix.Close(fd)
	if err != nil {
		log.Printf("Failed to close file descriptor %d: %v", fd, err)
	}
}

func safeClose(closable common.Closable) {
	err := closable.Close()
	if err != nil {
		log.Printf("Failed to close %v: %v", closable, err)
	}
}
