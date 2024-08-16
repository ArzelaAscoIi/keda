package scalers

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	v2 "k8s.io/api/autoscaling/v2"
	"k8s.io/metrics/pkg/apis/external_metrics"
)

type mockKubernetesWorkloadScaler struct {
	shouldError bool
	isActive    bool
	value       int64
}

func (m *mockKubernetesWorkloadScaler) Close(context.Context) error {
	return nil
}

func (m *mockKubernetesWorkloadScaler) GetMetricSpecForScaling(ctx context.Context) []v2.MetricSpec {
	return []v2.MetricSpec{
		{
			External: &v2.ExternalMetricSource{
				Metric: v2.MetricIdentifier{
					Name: "mock-kubernetes-workload",
				},
				Target: GetMetricTarget(v2.MetricTargetTypeAverageValue, 10),
			},
			Type: "External",
		},
	}
}

func (m *mockKubernetesWorkloadScaler) GetMetricsAndActivity(ctx context.Context, metricName string) ([]external_metrics.ExternalMetricValue, bool, error) {
	if m.shouldError {
		return nil, false, errors.New("mock error")
	}

	metric := GenerateMetricInMili(metricName, float64(m.value))
	return []external_metrics.ExternalMetricValue{metric}, m.isActive, nil
}

type mockAWSSqsQueueScaler struct {
	shouldError bool
	isActive    bool
	value       int64
}

func (m *mockAWSSqsQueueScaler) Close(context.Context) error {
	return nil
}

func (m *mockAWSSqsQueueScaler) GetMetricSpecForScaling(ctx context.Context) []v2.MetricSpec {
	return []v2.MetricSpec{
		{
			External: &v2.ExternalMetricSource{
				Metric: v2.MetricIdentifier{
					Name: "mock-aws-sqs",
				},
				Target: GetMetricTarget(v2.MetricTargetTypeAverageValue, 20),
			},
			Type: "External",
		},
	}
}

func (m *mockAWSSqsQueueScaler) GetMetricsAndActivity(ctx context.Context, metricName string) ([]external_metrics.ExternalMetricValue, bool, error) {
	if m.shouldError {
		return nil, false, errors.New("mock error")
	}

	metric := GenerateMetricInMili(metricName, float64(m.value))
	return []external_metrics.ExternalMetricValue{metric}, m.isActive, nil
}

func TestCombinedScaler(t *testing.T) {
	tests := []struct {
		name                string
		k8sScaler           Scaler
		awsScaler           Scaler
		expectedMetricValue int64
		expectedActive      bool
		expectError         bool
	}{
		{
			name: "Kubernetes scaler active, AWS scaler inactive",
			k8sScaler: &mockKubernetesWorkloadScaler{
				shouldError: false,
				isActive:    true,
				value:       10,
			},
			awsScaler: &mockAWSSqsQueueScaler{
				shouldError: false,
				isActive:    false,
				value:       20,
			},
			expectedMetricValue: 10,
			expectedActive:      true,
			expectError:         false,
		},
		{
			name: "Kubernetes scaler inactive, AWS scaler active",
			k8sScaler: &mockKubernetesWorkloadScaler{
				shouldError: false,
				isActive:    false,
				value:       10,
			},
			awsScaler: &mockAWSSqsQueueScaler{
				shouldError: false,
				isActive:    true,
				value:       20,
			},
			expectedMetricValue: 30, // 10 (k8s) + 20 (aws)
			expectedActive:      true,
			expectError:         false,
		},
		{
			name: "Both scalers inactive",
			k8sScaler: &mockKubernetesWorkloadScaler{
				shouldError: false,
				isActive:    false,
				value:       10,
			},
			awsScaler: &mockAWSSqsQueueScaler{
				shouldError: false,
				isActive:    false,
				value:       20,
			},
			expectedMetricValue: 30, // 10 (k8s) + 20 (aws)
			expectedActive:      false,
			expectError:         false,
		},
		{
			name: "Kubernetes scaler errors",
			k8sScaler: &mockKubernetesWorkloadScaler{
				shouldError: true,
			},
			awsScaler: &mockAWSSqsQueueScaler{
				shouldError: false,
				isActive:    false,
				value:       20,
			},
			expectError: true,
		},
		{
			name: "AWS scaler errors",
			k8sScaler: &mockKubernetesWorkloadScaler{
				shouldError: false,
				isActive:    false,
				value:       10,
			},
			awsScaler: &mockAWSSqsQueueScaler{
				shouldError: true,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scaler := &combinedScaler{
				kubernetesWorkloadScaler: tt.k8sScaler,
				awsSqsQueueScaler:        tt.awsScaler,
				logger:                   logr.Discard(),
			}

			ctx := context.Background()

			metrics, active, err := scaler.GetMetricsAndActivity(ctx, "MetricName")
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedActive, active)
				assert.EqualValues(t, tt.expectedMetricValue, metrics[0].Value.Value())
			}
		})
	}
}

func TestCombinedScalerGetMetricSpecForScaling(t *testing.T) {
	scaler := &combinedScaler{
		kubernetesWorkloadScaler: &mockKubernetesWorkloadScaler{
			shouldError: false,
		},
		awsSqsQueueScaler: &mockAWSSqsQueueScaler{
			shouldError: false,
		},
		logger: logr.Discard(),
	}

	ctx := context.Background()
	metricSpecs := scaler.GetMetricSpecForScaling(ctx)
	assert.Len(t, metricSpecs, 2) // Expecting two metrics: one from Kubernetes scaler and one from AWS SQS scaler
	assert.Equal(t, "mock-kubernetes-workload", metricSpecs[0].External.Metric.Name)
	assert.Equal(t, "mock-aws-sqs", metricSpecs[1].External.Metric.Name)
}
