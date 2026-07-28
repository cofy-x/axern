package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"

	artifactkernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/artifact"
)

type resolverFunc func(context.Context, string, int64) (artifactkernel.Resolved, error)

func (fn resolverFunc) ResolveArtifactTicket(ctx context.Context, ticket string, offset int64) (artifactkernel.Resolved, error) {
	return fn(ctx, ticket, offset)
}

type readerFunc func(context.Context, string, map[string]string) (artifactkernel.Upstream, error)

func (fn readerFunc) Open(ctx context.Context, url string, headers map[string]string) (artifactkernel.Upstream, error) {
	return fn(ctx, url, headers)
}

func TestDownloadStreamsAndVerifiesArtifact(t *testing.T) {
	payload := []byte("verified artifact bytes")
	sum := sha256.Sum256(payload)
	service := New(
		resolverFunc(func(context.Context, string, int64) (artifactkernel.Resolved, error) {
			return artifactkernel.Resolved{Size: int64(len(payload)), Digest: "sha256:" + hex.EncodeToString(sum[:]), URL: "https://object.internal/signed"}, nil
		}),
		readerFunc(func(context.Context, string, map[string]string) (artifactkernel.Upstream, error) {
			return artifactkernel.Upstream{Body: io.NopCloser(bytes.NewReader(payload))}, nil
		}),
		Options{MaxConcurrent: 1, ChunkBytes: 4, MaxBytes: 1024},
	)
	var got []byte
	var next int64
	if err := service.Download(context.Background(), "ticket", 0, func(offset int64, data []byte) error {
		if offset != next {
			t.Fatalf("offset=%d want=%d", offset, next)
		}
		next += int64(len(data))
		got = append(got, data...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload=%q", got)
	}
}

func TestDownloadResumeRequiresRangeAndExactSize(t *testing.T) {
	payload := []byte("456789")
	service := New(
		resolverFunc(func(_ context.Context, _ string, offset int64) (artifactkernel.Resolved, error) {
			if offset != 4 {
				t.Fatalf("offset=%d", offset)
			}
			return artifactkernel.Resolved{Size: 10, Digest: "sha256:unused", URL: "https://object.internal"}, nil
		}),
		readerFunc(func(context.Context, string, map[string]string) (artifactkernel.Upstream, error) {
			return artifactkernel.Upstream{Body: io.NopCloser(bytes.NewReader(payload)), Range: true, RangeStart: 4, RangeEnd: 9, RangeSize: 10}, nil
		}),
		Options{},
	)
	if err := service.Download(context.Background(), "ticket", 4, func(int64, []byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadRejectsCorruptionAndLengthMismatch(t *testing.T) {
	tests := []struct {
		name    string
		size    int64
		digest  string
		payload []byte
		want    error
	}{
		{"truncated", 4, "sha256:unused", []byte("abc"), ErrTruncated},
		{"excess", 2, "sha256:unused", []byte("abc"), ErrExcess},
		{"digest", 3, "sha256:0000", []byte("abc"), ErrDigest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := New(
				resolverFunc(func(context.Context, string, int64) (artifactkernel.Resolved, error) {
					return artifactkernel.Resolved{Size: test.size, Digest: test.digest}, nil
				}),
				readerFunc(func(context.Context, string, map[string]string) (artifactkernel.Upstream, error) {
					return artifactkernel.Upstream{Body: io.NopCloser(bytes.NewReader(test.payload))}, nil
				}),
				Options{},
			)
			if err := service.Download(context.Background(), "ticket", 0, func(int64, []byte) error { return nil }); !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestDownloadConcurrencyAndCancellationReleaseResources(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	payload := []byte("x")
	sum := sha256.Sum256(payload)
	service := New(
		resolverFunc(func(context.Context, string, int64) (artifactkernel.Resolved, error) {
			return artifactkernel.Resolved{Size: 1, Digest: "sha256:" + hex.EncodeToString(sum[:])}, nil
		}),
		readerFunc(func(context.Context, string, map[string]string) (artifactkernel.Upstream, error) {
			close(started)
			<-release
			return artifactkernel.Upstream{Body: io.NopCloser(bytes.NewReader(payload))}, nil
		}),
		Options{MaxConcurrent: 1},
	)
	done := make(chan error, 1)
	go func() {
		done <- service.Download(context.Background(), "first", 0, func(int64, []byte) error { return nil })
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first download did not start")
	}
	if err := service.Download(context.Background(), "second", 0, func(int64, []byte) error { return nil }); !errors.Is(err, ErrConcurrency) {
		t.Fatalf("second error=%v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
