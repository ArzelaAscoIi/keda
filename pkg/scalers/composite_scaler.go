package scalers

import (
	"context"
	"fmt"

	v2 "k8s.io/api/autoscaling/v2"
	"k8s.io/metrics/pkg/apis/external_metrics"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	"github.com/kedacore/keda/v2/pkg/scalers/scalersconfig"
)

type combinedScaler struct {
	metricType               v2.MetricTargetType
	kubernetesWorkloadScaler Scaler
	awsSqsQueueScaler        Scaler
	logger                   logr.Logger
}

func NewCompositeScaler(ctx context.Context, kubeClient client.Client, config *scalersconfig.ScalerConfig) (Scaler, error) {
	metricType, err := GetMetricTargetType(config)
	if err != nil {
		return nil, fmt.Errorf("error getting scaler metric type: %w", err)
	}

	logger := InitializeLogger(config, "combined_scaler")

	k8sScaler, err := NewKubernetesWorkloadScaler(kubeClient, config)
	if err != nil {
		return nil, fmt.Errorf("error creating Kubernetes workload scaler: %w", err)
	}

	awsScaler, err := NewAwsSqsQueueScaler(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("error creating AWS SQS queue scaler: %w", err)
	}

	return &combinedScaler{
		metricType:               metricType,
		kubernetesWorkloadScaler: k8sScaler,
		awsSqsQueueScaler:        awsScaler,
		logger:                   logger,
	}, nil
}

// Close closes both the Kubernetes workload scaler and the AWS SQS queue scaler.
func (s *combinedScaler) Close(ctx context.Context) error {
	if err := s.kubernetesWorkloadScaler.Close(ctx); err != nil {
		return err
	}
	return s.awsSqsQueueScaler.Close(ctx)
}

// GetMetricSpecForScaling returns the combined metric specs from both scalers.
func (s *combinedScaler) GetMetricSpecForScaling(ctx context.Context) []v2.MetricSpec {
	k8sMetricSpec := s.kubernetesWorkloadScaler.GetMetricSpecForScaling(ctx)
	awsMetricSpec := s.awsSqsQueueScaler.GetMetricSpecForScaling(ctx)
	return append(k8sMetricSpec, awsMetricSpec...)
}

// GetMetricsAndActivity runs the Kubernetes workload scaler first, then the AWS SQS queue scaler.
func (s *combinedScaler) GetMetricsAndActivity(ctx context.Context, metricName string) ([]external_metrics.ExternalMetricValue, bool, error) {
	k8sMetrics, k8sActive, err := s.kubernetesWorkloadScaler.GetMetricsAndActivity(ctx, metricName)
	if err != nil {
		return []external_metrics.ExternalMetricValue{}, false, err
	}

	if k8sActive {
		return k8sMetrics, true, nil
	}

	awsMetrics, awsActive, err := s.awsSqsQueueScaler.GetMetricsAndActivity(ctx, metricName)
	if err != nil {
		return []external_metrics.ExternalMetricValue{}, false, err
	}

	return append(k8sMetrics, awsMetrics...), awsActive, nil
}
