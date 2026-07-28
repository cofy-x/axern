package postgres

import (
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

type protoScanner interface {
	Scan(dest ...any) error
}

func scanVolumeClass(row protoScanner) (*storagev1.VolumeClass, error) {
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		return nil, err
	}
	out := &storagev1.VolumeClass{}
	if err := protojson.Unmarshal(payload, out); err != nil {
		return nil, err
	}
	return out, nil
}

func scanVolumeClaim(row protoScanner) (*storagev1.VolumeClaim, error) {
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		return nil, err
	}
	out := &storagev1.VolumeClaim{}
	if err := protojson.Unmarshal(payload, out); err != nil {
		return nil, err
	}
	return out, nil
}

func scanVolumeBinding(row protoScanner) (*privatestoragev1.VolumeBinding, error) {
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		return nil, err
	}
	out := &privatestoragev1.VolumeBinding{}
	if err := protojson.Unmarshal(payload, out); err != nil {
		return nil, err
	}
	return out, nil
}
