package pgsecret

import (
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func MarshalSecretForDebug(secret *secretv1.Secret) string {
	if secret == nil {
		return "{}"
	}
	data, err := protojson.Marshal(secret)
	if err != nil {
		return "{}"
	}
	return string(data)
}
