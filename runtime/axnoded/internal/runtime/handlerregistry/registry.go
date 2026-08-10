package handlerregistry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimecore "github.com/cofy-x/axern/runtime/axnoded/internal/runtime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/sirupsen/logrus"
)

type Status struct {
	Name         string
	Binary       string
	Loaded       bool
	Capabilities contract.RuntimeCapabilities
	Requirements contract.RuntimeRequirements
}

type Registry struct {
	config   config.Config
	handlers cmap.ConcurrentMap[string, contract.RuntimeHandler]
}

func New(cfg config.Config) *Registry {
	return &Registry{
		config:   cfg,
		handlers: cmap.New[contract.RuntimeHandler](),
	}
}

func (r *Registry) Map() cmap.ConcurrentMap[string, contract.RuntimeHandler] {
	if r == nil {
		return cmap.New[contract.RuntimeHandler]()
	}
	return r.handlers
}

func (r *Registry) Load(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("runtime handler registry is required")
	}
	if ctx == nil {
		return fmt.Errorf("runtime handler load context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("load configured runtime handlers: %w", err)
	}
	runtimeConfigs := r.config.PluginConfig.RuntimeConfig.NormalizedRuntimeConfigs()
	runtimeNames := make([]string, 0, len(runtimeConfigs))
	for runtimeName := range runtimeConfigs {
		runtimeNames = append(runtimeNames, runtimeName)
	}
	sort.Strings(runtimeNames)
	logrus.Debugf("loading runtime handlers: %v", runtimeConfigs)

	containersRoot := filepath.Join(r.config.RootDir, "containers")
	if err := os.MkdirAll(containersRoot, 0o755); err != nil {
		return fmt.Errorf("create runtime containers directory: %w", err)
	}

	backoff := 100 * time.Millisecond

	for {
		allLoaded := true
		for _, runtimeName := range runtimeNames {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("load configured runtime handlers: %w", err)
			}
			if r.handlers.Has(runtimeName) {
				continue
			}
			handler, err := runtimecore.GetRuntimeHandler(r.config, runtimeName)
			if err != nil {
				logrus.Warnf("load runtime %v handler failed: %v", runtimeName, err)
				allLoaded = false
				continue
			}
			logrus.Infof("loaded runtime handler for %v", runtimeName)
			r.handlers.Set(runtimeName, handler)
		}

		if allLoaded {
			return nil
		}

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return fmt.Errorf("load configured runtime handlers: %w", ctx.Err())
		case <-timer.C:
		}
		if backoff < 5*time.Second {
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
	}
}

func (r *Registry) Get(runtimeName string) (contract.RuntimeHandler, bool) {
	if r == nil {
		return nil, false
	}
	return r.handlers.Get(runtimeName)
}

func (r *Registry) Set(runtimeName string, handler contract.RuntimeHandler) {
	if r == nil {
		return
	}
	r.handlers.Set(runtimeName, handler)
}

func (r *Registry) Count() int {
	if r == nil {
		return 0
	}
	return r.handlers.Count()
}

func (r *Registry) Items() map[string]contract.RuntimeHandler {
	if r == nil {
		return map[string]contract.RuntimeHandler{}
	}
	return r.handlers.Items()
}

func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.handlers.Items()))
	for name := range r.handlers.Items() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Registry) Statuses() []Status {
	if r == nil {
		return nil
	}

	configured := r.config.PluginConfig.RuntimeConfig.NormalizedRuntimeConfigs()
	names := make([]string, 0, len(configured))
	for name := range configured {
		names = append(names, name)
	}
	for name := range r.handlers.Items() {
		if _, ok := configured[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	statuses := make([]Status, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		status := Status{Name: name}
		if runtimeCfg, ok := configured[name]; ok {
			status.Binary = runtimeCfg.Binary
		}
		if handler, ok := r.handlers.Get(name); ok {
			status.Loaded = true
			status.Capabilities = handler.Capabilities()
			status.Requirements = handler.Requirements()
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func (r *Registry) Version(ctx context.Context) ([]*runtimeapi.RuntimeVersion, error) {
	if r == nil {
		return nil, nil
	}
	versions := make([]*runtimeapi.RuntimeVersion, 0, r.handlers.Count())
	for runtimeName, handler := range r.handlers.Items() {
		v, err := handler.Version(ctx)
		if err != nil {
			logrus.Warnf("get runtime %s version failed: %v", runtimeName, err)
			versions = append(versions, &runtimeapi.RuntimeVersion{
				RuntimeName:    runtimeName,
				RuntimeVersion: config.UnknownVersion,
			})
			continue
		}
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].RuntimeName < versions[j].RuntimeName
	})
	return versions, nil
}

func Lookup(runtimeName string, registry *Registry) (contract.RuntimeHandler, error) {
	if registry == nil {
		return nil, fmt.Errorf("runtime %s is not supported", runtimeName)
	}
	handler, ok := registry.Get(runtimeName)
	if !ok {
		return nil, fmt.Errorf("runtime %s is not supported", runtimeName)
	}
	return handler, nil
}
