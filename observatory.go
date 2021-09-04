package libcore

import (
	"github.com/gogo/protobuf/jsonpb"
	"github.com/pkg/errors"
)

func (instance *V2RayInstance) GetObservatoryStatus() (string, error) {
	if instance.observatory == nil {
		return "", errors.New("observatory unavailable")
	}
	resp, err := instance.observatory.GetObservation(nil)
	if err != nil {
		return "", err
	}
	return (&jsonpb.Marshaler{
		EmitDefaults: true,
	}).MarshalToString(resp)
}
