package taskset

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

type PublishParams struct {
	Bundle       string
	Target       string
	Publisher    string
	KovaEndpoint string
	KovaToken    string
	Preheat      bool
}

type PublishResult struct {
	DescriptorReference string              `json:"descriptor_reference"`
	DescriptorDigest    string              `json:"descriptor_digest"`
	SourceDigest        string              `json:"source_digest"`
	Payloads            []PayloadDescriptor `json:"payloads"`
	KovaBuildID         string              `json:"kova_build_id,omitempty"`
}

type payloadPublishResult struct {
	Payloads []PayloadDescriptor
	BuildID  string
}

type payloadPublisher interface {
	Publish(context.Context, Resolved, string) (payloadPublishResult, error)
}

func Publish(ctx context.Context, params PublishParams) (PublishResult, error) {
	resolved, err := ResolveContext(ctx, params.Bundle)
	if err != nil {
		return PublishResult{}, err
	}
	target := strings.TrimSuffix(strings.TrimSpace(params.Target), "/")
	if target == "" {
		return PublishResult{}, fmt.Errorf("publish target is required")
	}
	repository, err := name.NewRepository(target, name.WeakValidation)
	if err != nil {
		return PublishResult{}, fmt.Errorf("publish target must be a registry repository without a tag or digest: %w", err)
	}
	target = repository.Name()
	publisherName := strings.TrimSpace(params.Publisher)
	if publisherName == "" {
		publisherName = "kova"
	}
	var publisher payloadPublisher
	switch publisherName {
	case "kova":
		publisher = kovaPublisher{endpoint: params.KovaEndpoint, token: params.KovaToken, preheat: params.Preheat}
	case "local":
		publisher = localPublisher{}
	default:
		return PublishResult{}, fmt.Errorf("publisher must be kova or local")
	}
	payloadRef := payloadReference(target, resolved.Descriptor.SourceDigest)
	published, err := publisher.Publish(ctx, resolved, payloadRef)
	if err != nil {
		return PublishResult{}, err
	}
	descriptor := resolved.Descriptor
	descriptor.Payloads = published.Payloads
	descriptorData, err := json.Marshal(descriptor)
	if err != nil {
		return PublishResult{}, err
	}
	descriptorRef := descriptorReference(target, descriptor.SourceDigest)
	digest, err := pushDescriptor(ctx, descriptorRef, descriptorData)
	if err != nil {
		return PublishResult{}, err
	}
	immutableDescriptorRef, err := immutableDigestReference(descriptorRef, digest)
	if err != nil {
		return PublishResult{}, err
	}
	result := PublishResult{
		DescriptorReference: immutableDescriptorRef,
		DescriptorDigest:    digest,
		SourceDigest:        descriptor.SourceDigest,
		Payloads:            published.Payloads,
		KovaBuildID:         published.BuildID,
	}
	if err := writeJSON(filepath.Join(params.Bundle, "published.json"), result); err != nil {
		return PublishResult{}, err
	}
	return result, nil
}

type localPublisher struct{}

func (localPublisher) Publish(ctx context.Context, resolved Resolved, target string) (payloadPublishResult, error) {
	index, err := layout.ImageIndexFromPath(filepath.Join(filepath.Dir(resolved.DescriptorPath), "oci"))
	if err != nil {
		return payloadPublishResult{}, fmt.Errorf("open payload OCI layout: %w", err)
	}
	manifest, err := index.IndexManifest()
	if err != nil || len(manifest.Manifests) != 1 {
		return payloadPublishResult{}, fmt.Errorf("payload OCI layout must contain exactly one image")
	}
	image, err := index.Image(manifest.Manifests[0].Digest)
	if err != nil {
		return payloadPublishResult{}, fmt.Errorf("read payload OCI image: %w", err)
	}
	ref, err := name.ParseReference(target, name.WeakValidation)
	if err != nil {
		return payloadPublishResult{}, err
	}
	if err := remote.Write(ref, image, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain)); err != nil {
		return payloadPublishResult{}, err
	}
	pushed, err := remote.Head(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return payloadPublishResult{}, fmt.Errorf("resolve pushed payload descriptor: %w", err)
	}
	immutableRef, err := immutableDigestReference(target, pushed.Digest.String())
	if err != nil {
		return payloadPublishResult{}, err
	}
	return payloadPublishResult{
		Payloads: []PayloadDescriptor{{
			Format:    "oci",
			Reference: immutableRef,
			Digest:    pushed.Digest.String(),
			MediaType: string(pushed.MediaType),
			SizeBytes: pushed.Size,
		}},
	}, nil
}

func pushDescriptor(ctx context.Context, reference string, descriptor []byte) (string, error) {
	image, err := newDescriptorImage(descriptor)
	if err != nil {
		return "", err
	}
	ref, err := name.ParseReference(reference, name.WeakValidation)
	if err != nil {
		return "", err
	}
	if err := remote.Write(ref, image, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain)); err != nil {
		return "", err
	}
	pushed, err := remote.Head(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return "", fmt.Errorf("resolve pushed TaskSet descriptor: %w", err)
	}
	return pushed.Digest.String(), nil
}

func newDescriptorImage(descriptor []byte) (v1.Image, error) {
	layer := static.NewLayer(descriptor, types.MediaType(DescriptorMediaType))
	image, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		return nil, err
	}
	image = mutate.ConfigMediaType(image, types.OCIConfigJSON)
	image = mutate.MediaType(image, types.OCIManifestSchema1)
	return image, nil
}

func payloadReference(target, sourceDigest string) string {
	return target + ":payload-" + digestShort(sourceDigest)
}
func descriptorReference(target, sourceDigest string) string {
	return target + ":taskset-" + digestShort(sourceDigest)
}
func digestShort(digest string) string {
	value := strings.TrimPrefix(digest, "sha256:")
	if len(value) > 16 {
		return value[:16]
	}
	return value
}

func immutableDigestReference(reference, digest string) (string, error) {
	parsed, err := name.ParseReference(reference, name.WeakValidation)
	if err != nil {
		return "", err
	}
	result := parsed.Context().Name() + "@" + digest
	if _, err := name.NewDigest(result, name.WeakValidation); err != nil {
		return "", err
	}
	return result, nil
}
