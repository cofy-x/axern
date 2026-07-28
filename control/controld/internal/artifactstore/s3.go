package artifactstore

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	rolloutkernel "github.com/cofy-x/axern/control/controld/internal/kernel/rollout"
)

type S3Config struct {
	Endpoint, Region, Bucket, AccessKey, SecretKey string
	UsePathStyle                                   bool
}
type S3 struct {
	bucket  string
	client  *s3.Client
	presign *s3.PresignClient
}

func NewS3(ctx context.Context, config S3Config) (*S3, error) {
	if strings.TrimSpace(config.Region) == "" || strings.TrimSpace(config.Bucket) == "" {
		return nil, fmt.Errorf("artifact S3 region and bucket are required")
	}
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(config.Region)}
	if config.AccessKey != "" || config.SecretKey != "" {
		if config.AccessKey == "" || config.SecretKey == "" {
			return nil, fmt.Errorf("artifact S3 access key and secret key must be configured together")
		}
		options = append(options, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(config.AccessKey, config.SecretKey, "")))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(cfg, func(value *s3.Options) {
		value.UsePathStyle = config.UsePathStyle
		if config.Endpoint != "" {
			value.BaseEndpoint = aws.String(config.Endpoint)
		}
	})
	return &S3{bucket: config.Bucket, client: client, presign: s3.NewPresignClient(client)}, nil
}
func (s *S3) PresignUpload(ctx context.Context, key, mediaType string, size int64, digest string, ttl time.Duration) (rolloutkernel.ArtifactUpload, error) {
	checksum, err := checksumBase64(digest)
	if err != nil {
		return rolloutkernel.ArtifactUpload{}, err
	}
	result, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ContentLength: aws.Int64(size), ContentType: aws.String(mediaType), ChecksumSHA256: aws.String(checksum), Metadata: map[string]string{"axern-sha256": digest}}, func(options *s3.PresignOptions) { options.Expires = ttl })
	if err != nil {
		return rolloutkernel.ArtifactUpload{}, err
	}
	headers := map[string]string{}
	for key, values := range result.SignedHeader {
		headers[key] = strings.Join(values, ",")
	}
	return rolloutkernel.ArtifactUpload{URL: result.URL, Headers: headers, ExpiresAt: time.Now().UTC().Add(ttl)}, nil
}
func (s *S3) Verify(ctx context.Context, key string, size int64, digest string) error {
	expected, err := checksumBase64(digest)
	if err != nil {
		return err
	}
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled})
	if err != nil {
		return err
	}
	if result.ContentLength == nil || *result.ContentLength != size {
		return fmt.Errorf("artifact size mismatch")
	}
	if result.ChecksumSHA256 != nil && *result.ChecksumSHA256 != expected {
		return fmt.Errorf("artifact sha256 checksum mismatch")
	}
	// Some S3-compatible stores validate x-amz-checksum-sha256 on PUT but do
	// not return it from HEAD. The digest metadata is part of the signed PUT
	// request and provides a portable verification fallback.
	if result.ChecksumSHA256 == nil && result.Metadata["axern-sha256"] != digest {
		return fmt.Errorf("artifact digest metadata mismatch")
	}
	return nil
}
func (s *S3) PresignDownload(ctx context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	result, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}, func(options *s3.PresignOptions) { options.Expires = ttl })
	if err != nil {
		return "", time.Time{}, err
	}
	return result.URL, time.Now().UTC().Add(ttl), nil
}
func (s *S3) DeletePrefix(ctx context.Context, prefix string) error {
	var token *string
	for {
		listed, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix), ContinuationToken: token})
		if err != nil {
			return err
		}
		if len(listed.Contents) > 0 {
			objects := make([]types.ObjectIdentifier, 0, len(listed.Contents))
			for _, object := range listed.Contents {
				objects = append(objects, types.ObjectIdentifier{Key: object.Key})
			}
			if _, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: aws.String(s.bucket), Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)}}); err != nil {
				return err
			}
		}
		if !aws.ToBool(listed.IsTruncated) {
			return nil
		}
		token = listed.NextContinuationToken
	}
}
func checksumBase64(digest string) (string, error) {
	value := strings.TrimPrefix(strings.TrimSpace(digest), "sha256:")
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("artifact digest must be sha256:<64 hex>")
	}
	return base64.StdEncoding.EncodeToString(decoded), nil
}
