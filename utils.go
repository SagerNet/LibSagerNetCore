package libcore

import (
	"github.com/v2fly/v2ray-core/v4/common"
	"log"
)

func safeClose(closable common.Closable) {
	err := closable.Close()
	if err != nil {
		log.Printf("Failed to close %v: %v", closable, err)
	}
}
