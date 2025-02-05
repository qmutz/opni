package drivers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/banzaicloud/k8s-objectmatcher/patch"
	"github.com/rancher/opni/apis"
	corev1beta1 "github.com/rancher/opni/apis/core/v1beta1"
	corev1 "github.com/rancher/opni/pkg/apis/core/v1"
	"github.com/rancher/opni/pkg/util/k8sutil"
	"github.com/rancher/opni/plugins/metrics/pkg/apis/cortexops"
	"github.com/samber/lo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type OpniManager struct {
	OpniManagerClusterDriverOptions
	cortexops.UnsafeCortexOpsServer
}

type OpniManagerClusterDriverOptions struct {
	k8sClient         client.Client
	monitoringCluster types.NamespacedName
	gatewayRef        types.NamespacedName
}

type OpniManagerClusterDriverOption func(*OpniManagerClusterDriverOptions)

func (o *OpniManagerClusterDriverOptions) apply(opts ...OpniManagerClusterDriverOption) {
	for _, op := range opts {
		op(o)
	}
}

func WithK8sClient(k8sClient client.Client) OpniManagerClusterDriverOption {
	return func(o *OpniManagerClusterDriverOptions) {
		o.k8sClient = k8sClient
	}
}

func WithMonitoringCluster(namespacedName types.NamespacedName) OpniManagerClusterDriverOption {
	return func(o *OpniManagerClusterDriverOptions) {
		o.monitoringCluster = namespacedName
	}
}

func WithGatewayRef(gatewayRef types.NamespacedName) OpniManagerClusterDriverOption {
	return func(o *OpniManagerClusterDriverOptions) {
		o.gatewayRef = gatewayRef
	}
}

func NewOpniManagerClusterDriver(opts ...OpniManagerClusterDriverOption) (*OpniManager, error) {
	options := OpniManagerClusterDriverOptions{
		monitoringCluster: types.NamespacedName{
			Namespace: os.Getenv("POD_NAMESPACE"),
			Name:      "opni",
		},
		gatewayRef: types.NamespacedName{
			Namespace: os.Getenv("POD_NAMESPACE"),
			Name:      os.Getenv("GATEWAY_NAME"),
		},
	}
	options.apply(opts...)
	if options.k8sClient == nil {
		c, err := k8sutil.NewK8sClient(k8sutil.ClientOptions{
			Scheme: apis.NewScheme(),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
		}
		options.k8sClient = c
	}
	return &OpniManager{
		OpniManagerClusterDriverOptions: options,
	}, nil
}

var _ ClusterDriver = (*OpniManager)(nil)

func (k *OpniManager) Name() string {
	return "opni-manager"
}

func (k *OpniManager) newMonitoringCluster() *corev1beta1.MonitoringCluster {
	return &corev1beta1.MonitoringCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      k.monitoringCluster.Name,
			Namespace: k.monitoringCluster.Namespace,
		},
	}
}

func (k *OpniManager) GetClusterConfiguration(ctx context.Context, _ *emptypb.Empty) (*cortexops.ClusterConfiguration, error) {
	mc := k.newMonitoringCluster()
	err := k.k8sClient.Get(ctx, client.ObjectKeyFromObject(mc), mc)
	if err != nil {
		return nil, err
	}
	storage := mc.Spec.Cortex.Storage.DeepCopy()
	storage.RedactSecrets()
	return &cortexops.ClusterConfiguration{
		Mode:    cortexops.DeploymentMode(cortexops.DeploymentMode_value[string(mc.Spec.Cortex.DeploymentMode)]),
		Storage: storage,
		Grafana: &cortexops.GrafanaConfig{
			Enabled:  &mc.Spec.Grafana.Enabled,
			Hostname: mc.Spec.Grafana.Hostname,
		},
	}, nil
}

func (k *OpniManager) ConfigureCluster(ctx context.Context, conf *cortexops.ClusterConfiguration) (*emptypb.Empty, error) {
	cluster := k.newMonitoringCluster()

	objectKey := client.ObjectKeyFromObject(cluster)
	err := k.k8sClient.Get(ctx, objectKey, cluster)
	exists := true
	if err != nil {
		if k8serrors.IsNotFound(err) {
			exists = false
		} else {
			return nil, fmt.Errorf("failed to get monitoring cluster: %w", err)
		}
	}

	// look up the gateway so we can set it as an owner reference
	gateway := &corev1beta1.Gateway{}
	err = k.k8sClient.Get(ctx, k.gatewayRef, gateway)
	if err != nil {
		return nil, fmt.Errorf("failed to get gateway: %w", err)
	}
	defaultGrafanaHostname := "grafana." + gateway.Spec.Hostname

	if conf.Grafana == nil {
		conf.Grafana = &cortexops.GrafanaConfig{
			Enabled: lo.ToPtr(true),
		}
	}
	if conf.Grafana.Enabled == nil {
		conf.Grafana.Enabled = lo.ToPtr(true)
	}
	if conf.Grafana.Hostname == "" {
		conf.Grafana.Hostname = defaultGrafanaHostname
	}
	if conf.Storage != nil && conf.Storage.RetentionPeriod != nil {
		retention := conf.Storage.RetentionPeriod.AsDuration()
		if retention > 0 && retention < 2*time.Hour {
			return nil, fmt.Errorf("storage retention period must be at least 2 hours")
		}
	}

	mutator := func(cluster *corev1beta1.MonitoringCluster) error {
		if err := conf.GetStorage().UnredactSecrets(cluster.Spec.Cortex.Storage); err != nil {
			return err
		}
		cluster.Spec.Cortex.Enabled = true
		cluster.Spec.Cortex.Storage = conf.GetStorage()
		if cluster.Spec.Cortex.Storage.Filesystem != nil &&
			cluster.Spec.Cortex.Storage.Filesystem.Directory == "" {
			cluster.Spec.Cortex.Storage.Filesystem.Directory = "/data"
		}
		cluster.Spec.Grafana.Enabled = *conf.Grafana.Enabled
		cluster.Spec.Grafana.Hostname = conf.Grafana.Hostname
		cluster.Spec.Gateway = v1.LocalObjectReference{
			Name: k.gatewayRef.Name,
		}
		cluster.Spec.Cortex.DeploymentMode = corev1beta1.DeploymentMode(cortexops.DeploymentMode_name[int32(conf.GetMode())])
		controllerutil.SetOwnerReference(gateway, cluster, k.k8sClient.Scheme())
		return nil
	}

	if exists {
		err := retry.OnError(retry.DefaultBackoff, k8serrors.IsConflict, func() error {
			existing := k.newMonitoringCluster()
			err := k.k8sClient.Get(ctx, objectKey, existing)
			if err != nil {
				return err
			}
			clone := existing.DeepCopy()
			if err := mutator(clone); err != nil {
				return err
			}
			cmp, err := patch.DefaultPatchMaker.Calculate(existing, clone,
				patch.IgnoreStatusFields(),
				patch.IgnoreVolumeClaimTemplateTypeMetaAndStatus(),
				patch.IgnorePDBSelector(),
			)
			if err == nil {
				if cmp.IsEmpty() {
					return status.Error(codes.FailedPrecondition, "no changes to apply")
				}
			}

			return k.k8sClient.Update(ctx, clone)
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update monitoring cluster: %w", err)
		}
	} else {
		if err := mutator(cluster); err != nil {
			return nil, err
		}
		err := k.k8sClient.Create(ctx, cluster)
		if err != nil {
			return nil, fmt.Errorf("failed to create monitoring cluster: %w", err)
		}
	}

	return &emptypb.Empty{}, nil
}

func (k *OpniManager) GetClusterStatus(ctx context.Context, _ *emptypb.Empty) (*cortexops.InstallStatus, error) {
	metadata := map[string]string{}
	var state cortexops.InstallState
	var version string

	cluster := k.newMonitoringCluster()
	err := k.k8sClient.Get(ctx, k.monitoringCluster, cluster)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			state = cortexops.InstallState_NotInstalled
		} else {
			return nil, fmt.Errorf("failed to get monitoring cluster: %w", err)
		}
	} else {
		status := cluster.Status.Cortex
		if err != nil {
			return nil, err
		}
		version = status.Version
		if cluster.GetDeletionTimestamp() != nil {
			state = cortexops.InstallState_Uninstalling
		} else {
			if status.WorkloadsReady {
				state = cortexops.InstallState_Installed
			} else {
				state = cortexops.InstallState_Updating
				metadata["Conditions"] = strings.Join(status.Conditions, "; ")
			}
		}
	}

	return &cortexops.InstallStatus{
		State:   state,
		Version: version,
		Metadata: lo.Assign(metadata, map[string]string{
			"Driver": k.Name(),
		}),
	}, nil
}

func (k *OpniManager) UninstallCluster(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	cluster := k.newMonitoringCluster()
	err := k.k8sClient.Get(ctx, k.monitoringCluster, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to uninstall monitoring cluster: %w", err)
	}

	err = k.k8sClient.Delete(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to uninstall monitoring cluster: %w", err)
	}

	return &emptypb.Empty{}, nil
}

func (k *OpniManager) ShouldDisableNode(_ *corev1.Reference) error {
	stat, err := k.GetClusterStatus(context.TODO(), &emptypb.Empty{})
	if err != nil {
		// can't determine cluster status, so don't disable the node
		return nil
	}
	switch stat.State {
	case cortexops.InstallState_NotInstalled, cortexops.InstallState_Uninstalling:
		return status.Error(codes.Unavailable, fmt.Sprintf("Cortex cluster is not installed"))
	case cortexops.InstallState_Updating, cortexops.InstallState_Installed:
		return nil
	case cortexops.InstallState_Unknown:
		fallthrough
	default:
		// can't determine cluster status, so don't disable the node
		return nil
	}
}
