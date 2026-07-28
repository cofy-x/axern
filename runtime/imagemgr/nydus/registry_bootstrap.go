package nydus

import (
	"context"
	"fmt"
	"time"

	"github.com/avast/retry-go/v4"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/sirupsen/logrus"
)

// bootstrapResult holds the output of a fetch+check+extract pipeline.
type bootstrapResult struct {
	Path            string
	Env             []string
	FetchDuration   time.Duration
	ConfigDuration  time.Duration
	CheckDuration   time.Duration
	ExtractDuration time.Duration
	ExtractTimings  bootstrapExtractionTimings
}

type imageConfigResult struct {
	Env      []string
	Duration time.Duration
}

// FetchAndExtractBootstrap fetches a Nydus image from registry and extracts bootstrap to outputDir.
// It also extracts environment variables from the image config.
func (c *RegistryClient) FetchAndExtractBootstrap(ctx context.Context, imageURL string, outputDir string) (string, []string, error) {
	return c.FetchAndExtractBootstrapWithDockerConfigJSON(ctx, imageURL, outputDir, "")
}

func (c *RegistryClient) FetchAndExtractBootstrapWithDockerConfigJSON(ctx context.Context, imageURL, outputDir, dockerConfigJSON string) (string, []string, error) {
	timing, ctx := StartNydusTimedOperation(ctx, "nydus.FetchAndExtractBootstrap", imageURL)
	defer timing.End()

	cache := c.getBootstrapCache()
	var result bootstrapResult

	stageStart := time.Now()
	cachePath, cachedEnv, hit, cacheErr := cache.Link(imageURL, outputDir)
	timing.Stage("bootstrap_cache_lookup", time.Since(stageStart))
	if cacheErr != nil {
		logrus.WithError(cacheErr).Warnf("failed to reuse cached Nydus bootstrap for %s", imageURL)
	} else if hit {
		result.Path = cachePath
		timing.Stage("bootstrap_cache_hit", time.Since(stageStart))

		if cachedEnv != nil {
			return result.Path, cachedEnv, nil
		}

		stageStart = time.Now()
		env := c.fetchEnvForCachedBootstrap(ctx, imageURL, dockerConfigJSON)
		timing.Stage("fetch_env_on_cache_hit", time.Since(stageStart))

		return result.Path, env, nil
	}

	err := retry.Do(
		func() error {
			r, err := c.fetchCheckAndExtract(ctx, imageURL, outputDir, dockerConfigJSON)
			if err != nil {
				return err
			}
			result = r
			return nil
		},
		retry.Attempts(nydusFetchRetryAttempts),
		retry.Delay(nydusFetchRetryDelay),
		retry.MaxDelay(nydusFetchRetryMaxDelay),
		retry.RetryIf(shouldRetryNydusFetch),
		retry.OnRetry(func(n uint, err error) {
			logrus.Warnf("Retry attempt %d for image %s: %v", n+1, imageURL, err)
		}),
		retry.Context(ctx),
	)

	if err != nil {
		timing.Fail(err)
		return "", nil, err
	}

	stageStart = time.Now()
	cacheErr = cache.Store(imageURL, outputDir, result.Path, result.Env)
	timing.Stage("bootstrap_cache_store", time.Since(stageStart))
	if cacheErr != nil {
		logrus.WithError(cacheErr).Warnf("failed to cache Nydus bootstrap for %s", imageURL)
	}

	timing.Stage("fetch_image", result.FetchDuration)
	timing.Stage("read_image_config", result.ConfigDuration)
	timing.Stage("check_nydus_format", result.CheckDuration)
	timing.Stage("extract_bootstrap", result.ExtractDuration)
	recordBootstrapExtractionTimings(timing, result.ExtractTimings)

	return result.Path, result.Env, nil
}

func (c *RegistryClient) fetchCheckAndExtract(ctx context.Context, imageURL, outputDir, dockerConfigJSON string) (bootstrapResult, error) {
	var r bootstrapResult

	stageStart := time.Now()
	img, err := c.fetchImageDirectAndAuth(ctx, imageURL, dockerConfigJSON)
	if err != nil {
		return r, fmt.Errorf("failed to fetch image %s: %w", imageURL, err)
	}
	r.FetchDuration = time.Since(stageStart)

	stageStart = time.Now()
	isNydus, err := c.isNydusImage(img)
	if err != nil {
		return r, fmt.Errorf("failed to check if image is Nydus: %w", err)
	}
	if !isNydus {
		return r, fmt.Errorf("image %s is not a Nydus image", imageURL)
	}
	r.CheckDuration = time.Since(stageStart)

	configResultCh := make(chan imageConfigResult, 1)
	go func() {
		configStart := time.Now()
		configResultCh <- imageConfigResult{
			Env:      c.imageEnv(img, imageURL, true),
			Duration: time.Since(configStart),
		}
	}()

	stageStart = time.Now()
	extraction, extractErr := c.extractBootstrap(ctx, img, outputDir)
	r.ExtractDuration = time.Since(stageStart)
	configResult := <-configResultCh
	r.Env = configResult.Env
	r.ConfigDuration = configResult.Duration
	if extractErr != nil {
		return r, fmt.Errorf("failed to extract bootstrap: %w", extractErr)
	}
	r.Path = extraction.Path
	r.ExtractTimings = extraction.Timings

	return r, nil
}

func recordBootstrapExtractionTimings(timing *NydusTimedOperation, timings bootstrapExtractionTimings) {
	timing.Stage("list_bootstrap_layers", timings.ListLayers)
	timing.Stage("prepare_bootstrap_output", timings.PrepareOutput)
	timing.Stage("open_bootstrap_stream", timings.OpenBootstrapStream)
	timing.Stage("scan_bootstrap_archive", timings.ScanBootstrapArchive)
	timing.Stage("copy_bootstrap_file", timings.CopyBootstrapFile)
}

func (c *RegistryClient) fetchEnvForCachedBootstrap(ctx context.Context, imageURL, dockerConfigJSON string) []string {
	img, err := c.fetchImageDirectAndAuth(ctx, imageURL, dockerConfigJSON)
	if err != nil {
		logrus.WithError(err).Debugf("bootstrap cache hit but failed to fetch image config for env: %s", imageURL)
		return nil
	}
	return c.imageEnv(img, imageURL, false)
}

func envFromImageConfig(img v1.Image, imageURL string, warnOnConfigError bool) []string {
	if img == nil {
		return nil
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		if warnOnConfigError {
			logrus.WithError(err).Warnf("failed to read image config for env extraction: %s", imageURL)
		} else {
			logrus.WithError(err).Debugf("bootstrap cache hit but failed to parse image config for env: %s", imageURL)
		}
		return nil
	}
	if cfg == nil {
		return nil
	}
	if len(cfg.Config.Env) == 0 {
		logrus.Debugf("image config has no env vars: %s", imageURL)
	}
	return cfg.Config.Env
}
