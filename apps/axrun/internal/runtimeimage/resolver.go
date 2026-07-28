package runtimeimage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

const (
	RepositoryEnv     = "AXERN_AXRUN_IMAGE_REPOSITORY"
	PushRepositoryEnv = "AXERN_AXRUN_IMAGE_PUSH_REPOSITORY"
	DeliveryEnv       = "AXERN_AXRUN_IMAGE_DELIVERY"
	ComposeProjectEnv = "COMPOSE_PROJECT_NAME"

	DeliveryRegistry         = "registry"
	DeliveryComposeImport    = "compose-import"
	defaultComposeProject    = "axern-local"
	defaultComposeRepository = "axrun/local-task"
)

type Request struct {
	RunDir  string
	Task    domain.TaskInstance
	Episode domain.Episode
}

type Result struct {
	Image          string `json:"image"`
	Repository     string `json:"repository"`
	PushImage      string `json:"push_image,omitempty"`
	PushRepository string `json:"push_repository,omitempty"`
	Delivery       string `json:"delivery"`
	ImportedRef    string `json:"imported_ref,omitempty"`
	Tag            string `json:"tag"`
	ContextRef     string `json:"context_ref"`
	DockerfileRef  string `json:"dockerfile_ref"`
	ContextDir     string `json:"-"`
	DockerfilePath string `json:"-"`
}

type Resolver interface {
	Resolve(context.Context, Request) (Result, error)
}

type DockerResolver struct {
	Repository     string
	PushRepository string
	Delivery       string
	ComposeProject string
	Runner         CommandRunner
}

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

func NewDockerResolverFromEnv() DockerResolver {
	return DockerResolver{
		Repository:     strings.TrimSpace(os.Getenv(RepositoryEnv)),
		PushRepository: strings.TrimSpace(os.Getenv(PushRepositoryEnv)),
		Delivery:       strings.TrimSpace(os.Getenv(DeliveryEnv)),
		ComposeProject: strings.TrimSpace(os.Getenv(ComposeProjectEnv)),
		Runner:         ExecCommandRunner{},
	}
}

func (r DockerResolver) Resolve(ctx context.Context, request Request) (Result, error) {
	source := request.Task.Sandbox.RuntimeSource
	if source == nil || source.Type != domain.SandboxRuntimeSourceDockerfile {
		return Result{}, fmt.Errorf("docker runtime image resolver requires dockerfile runtime_source")
	}
	repository := strings.TrimSpace(r.Repository)
	delivery := normalizeDelivery(r.Delivery)
	if err := validateDelivery(delivery); err != nil {
		return Result{}, err
	}
	if delivery == DeliveryComposeImport && repository == "" {
		repository = defaultComposeRepository
	}
	if err := ValidateRepository(repository); err != nil {
		return Result{}, err
	}
	pushRepository := strings.TrimSpace(r.PushRepository)
	if pushRepository == "" {
		pushRepository = repository
	}
	if err := validateRepository(pushRepository, PushRepositoryEnv); err != nil {
		return Result{}, err
	}
	runDir := strings.TrimSpace(request.RunDir)
	if runDir == "" {
		return Result{}, fmt.Errorf("run directory is required")
	}
	dockerfileRef, err := cleanRunRef(source.Dockerfile)
	if err != nil {
		return Result{}, fmt.Errorf("runtime Dockerfile ref: %w", err)
	}
	dockerfilePath := filepath.Join(runDir, filepath.FromSlash(dockerfileRef))
	info, err := os.Stat(dockerfilePath)
	if err != nil {
		return Result{}, fmt.Errorf("stat runtime Dockerfile %s: %w", source.Dockerfile, err)
	}
	if info.IsDir() {
		return Result{}, fmt.Errorf("runtime Dockerfile %s is a directory", source.Dockerfile)
	}
	contextDir := filepath.Dir(dockerfilePath)
	contextRef := filepath.ToSlash(filepath.Dir(dockerfileRef))
	if contextRef == "." {
		contextRef = ""
	}
	tag, err := ImageTag(request, contextDir, dockerfileRef)
	if err != nil {
		return Result{}, err
	}
	image := repository + ":" + tag
	pushImage := pushRepository + ":" + tag
	runner := r.Runner
	if runner == nil {
		runner = ExecCommandRunner{}
	}
	if output, err := runner.Run(ctx, "docker", "build", "-f", dockerfilePath, "-t", pushImage, contextDir); err != nil {
		return Result{}, fmt.Errorf("docker build %s: %w%s", pushImage, err, commandOutputSuffix(output))
	}
	importedRef := ""
	switch delivery {
	case DeliveryRegistry:
		if output, err := runner.Run(ctx, "docker", "push", pushImage); err != nil {
			return Result{}, fmt.Errorf("docker push %s: %w%s", pushImage, err, commandOutputSuffix(output))
		}
	case DeliveryComposeImport:
		ref, err := r.importCompose(ctx, runner, pushImage, repository)
		if err != nil {
			return Result{}, err
		}
		image = ref
		importedRef = ref
	default:
		return Result{}, fmt.Errorf("%s has unsupported value %q", DeliveryEnv, delivery)
	}
	return Result{
		Image:          image,
		Repository:     repository,
		PushImage:      pushImage,
		PushRepository: pushRepository,
		Delivery:       delivery,
		ImportedRef:    importedRef,
		Tag:            tag,
		ContextRef:     contextRef,
		ContextDir:     contextDir,
		DockerfilePath: dockerfilePath,
		DockerfileRef:  dockerfileRef,
	}, nil
}

func (r DockerResolver) importCompose(ctx context.Context, runner CommandRunner, localImage string, repository string) (string, error) {
	project := strings.TrimSpace(r.ComposeProject)
	if project == "" {
		project = defaultComposeProject
	}
	nodeContainer := project + "-node-1"
	archive, err := os.CreateTemp("", "axrun-runtime-image-*.tar")
	if err != nil {
		return "", fmt.Errorf("create runtime image archive: %w", err)
	}
	archivePath := archive.Name()
	if err := archive.Close(); err != nil {
		return "", fmt.Errorf("close runtime image archive: %w", err)
	}
	defer os.Remove(archivePath)
	remoteArchive := fmt.Sprintf("/tmp/axrun-runtime-image-%d.tar", time.Now().UnixNano())
	defer runner.Run(context.Background(), "docker", "exec", nodeContainer, "rm", "-f", remoteArchive)

	if output, err := runner.Run(ctx, "docker", "save", "-o", archivePath, localImage); err != nil {
		return "", fmt.Errorf("docker save %s: %w%s", localImage, err, commandOutputSuffix(output))
	}
	digest, err := fileSHA256Digest(archivePath)
	if err != nil {
		return "", fmt.Errorf("digest runtime image archive: %w", err)
	}
	imageRef := normalizeDigestRepository(repository) + "@" + digest
	if output, err := runner.Run(ctx, "docker", "cp", archivePath, nodeContainer+":"+remoteArchive); err != nil {
		return "", fmt.Errorf("copy runtime image archive into compose node: %w%s", err, commandOutputSuffix(output))
	}
	if output, err := runner.Run(ctx, "docker", "exec", nodeContainer, "axctl", "image", "import", "--imagemgr-socket", "/run/imagemgr/imagemgr.sock", "--archive", remoteArchive, "--ref", imageRef); err != nil {
		return "", fmt.Errorf("import runtime image %s into compose node: %w%s", imageRef, err, commandOutputSuffix(output))
	}
	return imageRef, nil
}

func ImageTag(request Request, contextDir string, dockerfileRef string) (string, error) {
	contextDigest, err := ContextDigest(contextDir)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(request.Episode.RunID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(request.Task.ID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(filepath.ToSlash(dockerfileRef)))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(contextDigest))
	sum := hex.EncodeToString(hash.Sum(nil))[:12]
	return fmt.Sprintf("axrun-%s-%s", safeTagPart(request.Task.ID), sum), nil
}

func ContextDigest(contextDir string) (string, error) {
	root := filepath.Clean(contextDir)
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = hash.Write([]byte(filepath.ToSlash(rel)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("digest runtime image context %s: %w", contextDir, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func ValidateRepository(repository string) error {
	return validateRepository(repository, RepositoryEnv)
}

func ValidateResolverConfig(resolver DockerResolver) error {
	repository := strings.TrimSpace(resolver.Repository)
	delivery := normalizeDelivery(resolver.Delivery)
	if err := validateDelivery(delivery); err != nil {
		return err
	}
	if delivery == DeliveryComposeImport && repository == "" {
		repository = defaultComposeRepository
	}
	if err := ValidateRepository(repository); err != nil {
		return err
	}
	pushRepository := strings.TrimSpace(resolver.PushRepository)
	if pushRepository == "" {
		return nil
	}
	return validateRepository(pushRepository, PushRepositoryEnv)
}

func normalizeDelivery(delivery string) string {
	delivery = strings.TrimSpace(delivery)
	if delivery == "" {
		return DeliveryRegistry
	}
	return delivery
}

func validateDelivery(delivery string) error {
	switch delivery {
	case DeliveryRegistry, DeliveryComposeImport:
		return nil
	default:
		return fmt.Errorf("%s has unsupported value %q", DeliveryEnv, delivery)
	}
}

func validateRepository(repository string, envName string) error {
	if repository == "" {
		return fmt.Errorf("%s is required for dockerfile runtime_source", envName)
	}
	if strings.Contains(repository, "@") {
		return fmt.Errorf("%s must be an image repository without digest", envName)
	}
	lastSlash := strings.LastIndex(repository, "/")
	lastColon := strings.LastIndex(repository, ":")
	if lastColon > lastSlash {
		return fmt.Errorf("%s must be an image repository without tag", envName)
	}
	return nil
}

func fileSHA256Digest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func normalizeDigestRepository(repository string) string {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return repository
	}
	first, _, hasSlash := strings.Cut(repository, "/")
	if hasSlash && (strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost") {
		return repository
	}
	if !hasSlash {
		return "index.docker.io/library/" + repository
	}
	return "index.docker.io/" + repository
}

func cleanRunRef(value string) (string, error) {
	ref := filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
	if ref == "" || ref == "." {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(ref) || ref == ".." || strings.HasPrefix(ref, "../") {
		return "", fmt.Errorf("path must be run-root-relative")
	}
	return ref, nil
}

var tagPartPattern = regexp.MustCompile(`[^a-z0-9_.-]+`)

func safeTagPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = tagPartPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, ".-")
	if value == "" {
		return "task"
	}
	if len(value) > 64 {
		return value[:64]
	}
	return value
}

func commandOutputSuffix(output []byte) string {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > 4000 {
		trimmed = trimmed[len(trimmed)-4000:]
	}
	return ": " + trimmed
}
