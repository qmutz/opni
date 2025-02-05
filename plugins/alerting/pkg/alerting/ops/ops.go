package ops

import (
	"context"
	"time"

	"github.com/rancher/opni/pkg/alerting/routing"

	"github.com/rancher/opni/pkg/alerting/shared"
	"github.com/rancher/opni/pkg/util/future"
	"github.com/rancher/opni/plugins/alerting/pkg/alerting/drivers"
	"github.com/rancher/opni/plugins/alerting/pkg/apis/alertops"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Manages all dynamic backend configurations
// that must interact with & modify the runtime cluster
type AlertingOpsNode struct {
	AlertingOpsNodeOptions
	ClusterDriver future.Future[drivers.ClusterDriver]
	alertops.UnsafeAlertingAdminServer
	alertops.UnsafeDynamicAlertingServer
}

type AlertingOpsNodeOptions struct {
	timeout time.Duration
}

type AlertingOpsNodeOption func(*AlertingOpsNodeOptions)

func (a *AlertingOpsNodeOptions) apply(opts ...AlertingOpsNodeOption) {
	for _, opt := range opts {
		opt(a)
	}
}

var _ alertops.AlertingAdminServer = (*AlertingOpsNode)(nil)

func NewAlertingOpsNode(clusterDriver future.Future[drivers.ClusterDriver], opts ...AlertingOpsNodeOption) *AlertingOpsNode {
	options := AlertingOpsNodeOptions{
		timeout: 60 * time.Second,
	}
	options.apply(opts...)

	return &AlertingOpsNode{
		AlertingOpsNodeOptions: options,
		ClusterDriver:          clusterDriver,
	}
}

func (a *AlertingOpsNode) GetClusterConfiguration(ctx context.Context, _ *emptypb.Empty) (*alertops.ClusterConfiguration, error) {
	ctxTimeout, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	driver, err := a.ClusterDriver.GetContext(ctxTimeout)
	if err != nil {
		return nil, err
	}
	return driver.GetClusterConfiguration(ctx, &emptypb.Empty{})

}

func (a *AlertingOpsNode) ConfigureCluster(ctx context.Context, conf *alertops.ClusterConfiguration) (*emptypb.Empty, error) {
	ctxTimeout, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	driver, err := a.ClusterDriver.GetContext(ctxTimeout)
	if err != nil {
		return nil, err
	}
	return driver.ConfigureCluster(ctx, conf)
}

func (a *AlertingOpsNode) GetClusterStatus(ctx context.Context, _ *emptypb.Empty) (*alertops.InstallStatus, error) {
	ctxTimeout, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	driver, err := a.ClusterDriver.GetContext(ctxTimeout)
	if err != nil {
		return nil, err
	}
	return driver.GetClusterStatus(ctx, &emptypb.Empty{})
}

func (a *AlertingOpsNode) UninstallCluster(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	ctxTimeout, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	driver, err := a.ClusterDriver.GetContext(ctxTimeout)
	if err != nil {
		return nil, err
	}
	return driver.UninstallCluster(ctx, &emptypb.Empty{})
}

func (a *AlertingOpsNode) InstallCluster(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	ctxTimeout, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	driver, err := a.ClusterDriver.GetContext(ctxTimeout)
	if err != nil {
		return nil, err
	}
	return driver.InstallCluster(ctx, &emptypb.Empty{})
}

func (a *AlertingOpsNode) Fetch(ctx context.Context, _ *emptypb.Empty) (*alertops.AlertingConfig, error) {
	ctxTimeout, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	driver, err := a.ClusterDriver.GetContext(ctxTimeout)
	if err != nil {
		return nil, err
	}
	return driver.Fetch(ctx, &emptypb.Empty{})
}

func (a *AlertingOpsNode) Update(ctx context.Context, config *alertops.AlertingConfig) (*emptypb.Empty, error) {
	ctxTimeout, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	driver, err := a.ClusterDriver.GetContext(ctxTimeout)
	if err != nil {
		return nil, err
	}
	return driver.Update(ctx, config)
}

func (a *AlertingOpsNode) Reload(ctx context.Context, info *alertops.ReloadInfo) (*emptypb.Empty, error) {
	ctxTimeout, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	driver, err := a.ClusterDriver.GetContext(ctxTimeout)
	if err != nil {
		return nil, err
	}
	return driver.Reload(ctx, info)
}

func (a *AlertingOpsNode) GetRuntimeOptions(ctx context.Context) (shared.NewAlertingOptions, error) {
	ctxTimeout, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	driver, err := a.ClusterDriver.GetContext(ctxTimeout)
	if err != nil {
		return shared.NewAlertingOptions{}, err
	}
	return driver.GetRuntimeOptions()
}

func (a *AlertingOpsNode) ConfigFromBackend(ctx context.Context) (*routing.RoutingTree, *routing.OpniInternalRouting, error) {
	ctxTimeout, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	driver, err := a.ClusterDriver.GetContext(ctxTimeout)
	if err != nil {
		return nil, nil, err
	}
	return driver.ConfigFromBackend(ctx)
}

func (a *AlertingOpsNode) ApplyConfigToBackend(
	ctx context.Context,
	config *routing.RoutingTree,
	internal *routing.OpniInternalRouting,
) error {
	ctxTimeout, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	driver, err := a.ClusterDriver.GetContext(ctxTimeout)
	if err != nil {
		return err
	}
	return driver.ApplyConfigToBackend(ctx, config, internal)
}

func (a *AlertingOpsNode) GetAvailableEndpoint(ctx context.Context, options shared.NewAlertingOptions) (string, error) {
	var availableEndpoint string
	status, err := a.GetClusterConfiguration(ctx, &emptypb.Empty{})
	if err != nil {
		return "", err
	}
	if status.NumReplicas == 1 { // exactly one that is the controller
		availableEndpoint = options.GetControllerEndpoint()
	} else {
		availableEndpoint = options.GetWorkerEndpoint()
	}
	return availableEndpoint, nil
}
