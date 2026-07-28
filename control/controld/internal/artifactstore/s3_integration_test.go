package artifactstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestS3ArtifactRoundTripIntegration(t *testing.T) {
	endpoint := os.Getenv("AXERN_TEST_ARTIFACT_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("AXERN_TEST_ARTIFACT_S3_ENDPOINT is not set")
	}
	store, err := NewS3(context.Background(), S3Config{
		Endpoint: endpoint, Region: envOr("AXERN_TEST_ARTIFACT_S3_REGION", "us-east-1"),
		Bucket: envOr("AXERN_TEST_ARTIFACT_S3_BUCKET", "axern-system"), AccessKey: os.Getenv("AXERN_TEST_ARTIFACT_S3_ACCESS_KEY"), SecretKey: os.Getenv("AXERN_TEST_ARTIFACT_S3_SECRET_KEY"), UsePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.client.HeadBucket(context.Background(), &s3.HeadBucketInput{Bucket: aws.String(store.bucket)}); err != nil {
		if _, createErr := store.client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String(store.bucket)}); createErr != nil {
			t.Fatalf("create integration bucket: %v (head: %v)", createErr, err)
		}
	}
	data := []byte("durable rollout evidence\n")
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	prefix := "integration-tests/" + time.Now().UTC().Format("20060102T150405.000000000") + "/"
	t.Cleanup(func() {
		if err := store.DeletePrefix(context.Background(), prefix); err != nil {
			t.Errorf("cleanup artifact prefix: %v", err)
		}
	})
	upload, err := store.PresignUpload(context.Background(), prefix+"evidence.txt", "text/plain", int64(len(data)), digest, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPut, upload.URL, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range upload.Headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode/100 != 2 {
		t.Fatalf("upload returned %s", response.Status)
	}
	if err := store.Verify(context.Background(), prefix+"evidence.txt", int64(len(data)), digest); err != nil {
		t.Fatal(err)
	}
	downloadURL, _, err := store.PresignDownload(context.Background(), prefix+"evidence.txt", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	download, err := http.Get(downloadURL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(download.Body)
	download.Body.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("download evidence: err=%v got=%q", err, got)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
