package app

import (
	"context"
	"fmt"
	"net/http"

	api "github.com/cofy-x/axern/control/storaged/internal/api"
	appstorage "github.com/cofy-x/axern/control/storaged/internal/application/storage"
	"github.com/cofy-x/axern/control/storaged/internal/postgres"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
)

type Config struct {
	PostgresDSN string
}

type App struct {
	db     *postgres.DB
	server *api.Server
}

func New(ctx context.Context, cfg Config) (*App, error) {
	db, err := postgres.Open(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, err
	}
	store := postgres.NewStore(db)
	controller := appstorage.NewController(store)
	if err := ensureDefaultClasses(ctx, controller); err != nil {
		db.Close()
		return nil, err
	}
	app := &App{
		db:     db,
		server: api.NewServer(controller),
	}
	return app, nil
}

func (a *App) Close() error {
	if a == nil {
		return nil
	}
	a.db.Close()
	return nil
}

func (a *App) Handler() *api.Server {
	if a == nil {
		return nil
	}
	return a.server
}

func (a *App) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	})
	return mux
}

func ensureDefaultClasses(ctx context.Context, controller *appstorage.Controller) error {
	if _, ok, err := controller.GetVolumeClass(ctx, "local"); err != nil {
		return err
	} else if ok {
		return nil
	}
	_, err := controller.CreateVolumeClass(ctx, &storagev1.CreateVolumeClassRequest{
		Name:                 "local",
		Backend:              storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
		AccessModes:          []storagev1.VolumeAccessMode{storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE},
		DefaultReclaimPolicy: storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_RETAIN,
		ConsistencyProfile:   storagev1.VolumeConsistencyProfile_VOLUME_CONSISTENCY_PROFILE_POSIX,
		RuntimeCompatibility: &storagev1.VolumeRuntimeCompatibility{
			SupportsRunc:  true,
			SupportsRunsc: true,
		},
	})
	if err == nil {
		return nil
	}
	if _, ok, getErr := controller.GetVolumeClass(ctx, "local"); getErr != nil {
		return getErr
	} else if ok {
		return nil
	}
	return err
}
