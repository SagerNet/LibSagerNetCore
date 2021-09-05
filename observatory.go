package libcore

import (
	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
)

func (instance *V2RayInstance) GetObservatoryStatus() ([]byte, error) {
	if instance.observatory == nil {
		return nil, errors.New("observatory unavailable")
	}
	resp, err := instance.observatory.GetObservation(nil)
	if err != nil {
		return nil, err
	}
	return proto.Marshal(resp)
}
