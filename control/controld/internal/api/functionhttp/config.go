package functionhttp

import "context"

type Config struct {
	ReadBundle func(ctx context.Context, storageURI string) (BundlePayload, bool, error)
	Token      string
}

type BundlePayload struct {
	Digest    string
	MediaType string
	SizeBytes int64
	Payload   []byte
}
