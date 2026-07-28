package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	artifactkernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/artifact"
)

type Options struct {
	MaxConcurrent, ChunkBytes int
	MaxBytes                  int64
	Observer                  Observer
}
type Observer interface {
	BeginArtifactDownload(bool) func(int64, string, string)
}
type Service struct {
	resolver   artifactkernel.TicketResolver
	reader     artifactkernel.ByteReader
	slots      chan struct{}
	chunkBytes int
	maxBytes   int64
	observer   Observer
}

func New(resolver artifactkernel.TicketResolver, reader artifactkernel.ByteReader, options Options) *Service {
	if options.MaxConcurrent <= 0 {
		options.MaxConcurrent = 16
	}
	if options.ChunkBytes <= 0 {
		options.ChunkBytes = 256 << 10
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = 8 << 30
	}
	return &Service{resolver: resolver, reader: reader, slots: make(chan struct{}, options.MaxConcurrent), chunkBytes: options.ChunkBytes, maxBytes: options.MaxBytes, observer: options.Observer}
}
func (s *Service) Download(ctx context.Context, ticket string, offset int64, send func(int64, []byte) error) (resultErr error) {
	if offset < 0 {
		return fmt.Errorf("offset must not be negative")
	}
	var streamed int64
	if s.observer != nil {
		finish := s.observer.BeginArtifactDownload(offset > 0)
		defer func() {
			result, errorClass := "ok", "none"
			if resultErr != nil {
				result, errorClass = "error", classify(resultErr)
			}
			finish(streamed, result, errorClass)
		}()
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return ErrConcurrency
	}
	resolved, err := s.resolver.ResolveArtifactTicket(ctx, ticket, offset)
	if err != nil {
		return err
	}
	if resolved.Size > s.maxBytes {
		return ErrTooLarge
	}
	if offset > resolved.Size {
		return fmt.Errorf("offset exceeds artifact size")
	}
	upstream, err := s.reader.Open(ctx, resolved.URL, resolved.Headers)
	if err != nil {
		return ErrUpstream
	}
	defer upstream.Body.Close()
	if offset > 0 && (!upstream.Range || upstream.RangeStart != offset || upstream.RangeEnd != resolved.Size-1 || upstream.RangeSize != resolved.Size) {
		return ErrRange
	}
	if offset == 0 && upstream.Range {
		return ErrUpstream
	}
	remaining := resolved.Size - offset
	limited := io.LimitReader(upstream.Body, remaining+1)
	buffer := make([]byte, s.chunkBytes)
	position := offset
	hasher := sha256.New()
	for {
		count, readErr := limited.Read(buffer)
		if count > 0 {
			if position+int64(count) > resolved.Size {
				return ErrExcess
			}
			if offset == 0 {
				_, _ = hasher.Write(buffer[:count])
			}
			if err := send(position, append([]byte(nil), buffer[:count]...)); err != nil {
				return err
			}
			position += int64(count)
			streamed += int64(count)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read artifact byte store: %w", readErr)
		}
	}
	if position != resolved.Size {
		return ErrTruncated
	}
	if offset == 0 {
		want := strings.TrimPrefix(resolved.Digest, "sha256:")
		got := hex.EncodeToString(hasher.Sum(nil))
		if want == "" || !strings.EqualFold(want, got) {
			return ErrDigest
		}
	}
	return nil
}

func classify(err error) string {
	switch {
	case errors.Is(err, ErrConcurrency):
		return "concurrency"
	case errors.Is(err, ErrTooLarge):
		return "size_limit"
	case errors.Is(err, ErrRange):
		return "range"
	case errors.Is(err, ErrExcess), errors.Is(err, ErrTruncated):
		return "size_mismatch"
	case errors.Is(err, ErrDigest):
		return "digest_mismatch"
	case errors.Is(err, ErrUpstream):
		return "upstream"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "unknown"
	}
}

type classifiedError string

func (e classifiedError) Error() string { return string(e) }

const (
	ErrConcurrency classifiedError = "artifact download concurrency limit reached"
	ErrTooLarge    classifiedError = "artifact exceeds gateway size limit"
	ErrRange       classifiedError = "artifact byte store did not honor resume range"
	ErrUpstream    classifiedError = "artifact byte store rejected download"
	ErrExcess      classifiedError = "artifact byte store returned excess data"
	ErrTruncated   classifiedError = "artifact byte store returned truncated data"
	ErrDigest      classifiedError = "artifact digest verification failed"
)
