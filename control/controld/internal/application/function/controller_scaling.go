package appfunction

import (
	"context"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (c *Controller) SweepIdleScaleDown(ctx context.Context, now time.Time) (int, error) {
	idle, err := c.store.ListIdleDeployments(ctx, now)
	if err != nil {
		return 0, err
	}
	scaled := 0
	for _, d := range idle {
		if err := c.scaleDownWorker(ctx, d, now); err != nil {
			logrus.WithError(err).WithField("function_id", d.FunctionID).Warn("idle scale-down failed")
			continue
		}
		scaled++
	}
	if scaled > 0 {
		logrus.WithField("count", scaled).Info("idle function scale-down sweep completed")
	}
	return scaled, nil
}

func (c *Controller) scaleDownWorker(ctx context.Context, d functionkernel.IdleDeployment, now time.Time) error {
	service, ok, err := c.services.Get(ctx, d.WorkerServiceID)
	if err != nil || !ok || service == nil {
		return err
	}
	replicas := d.MinReplicas
	scaled, err := c.services.Update(ctx, &servicev1.UpdateServiceRequest{
		ServiceID:       d.WorkerServiceID,
		ExpectedVersion: service.GetVersion(),
		Replicas:        &replicas,
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"replicas"}},
	}, now)
	if err != nil {
		return err
	}
	recorded, err := c.store.RecordScaleDown(ctx, d.FunctionID, replicas, now)
	if err != nil || recorded {
		return err
	}
	if scaled == nil {
		return nil
	}
	restore := d.DesiredReplicas
	_, err = c.services.Update(ctx, &servicev1.UpdateServiceRequest{
		ServiceID:       d.WorkerServiceID,
		ExpectedVersion: scaled.GetVersion(),
		Replicas:        &restore,
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"replicas"}},
	}, now)
	return err
}
