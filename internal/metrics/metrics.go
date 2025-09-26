// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	containerRecommendationExcess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vpa_butler_vpa_container_recommendation_excess",
		Help: "Subtracts the maximum allowed recommendation from the uncapped target recommendation per container",
	}, []string{"namespace", "verticalpodautoscaler", "container", "resource", "unit"})
)

var (
	containerMaxAllowed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vpa_butler_vpa_container_max_allowed",
		Help: "Max allowed value per container",
	}, []string{"namespace", "verticalpodautoscaler", "container", "resource", "unit"})
)

func RegisterMetrics() {
	metrics.Registry.MustRegister(containerRecommendationExcess)
	metrics.Registry.MustRegister(containerMaxAllowed)
}

func RecordContainerVpaMetrics(vpa *vpav1.VerticalPodAutoscaler) {
	// no policy => no maximum => no excess
	// no recommendations => no excess
	if vpa.Spec.ResourcePolicy == nil || vpa.Status.Recommendation == nil {
		return
	}

	labels := prometheus.Labels{
		"namespace":             vpa.Namespace,
		"verticalpodautoscaler": vpa.Name,
	}

	maxAllowed := make(map[string]corev1.ResourceList)
	for i := range vpa.Spec.ResourcePolicy.ContainerPolicies {
		policy := vpa.Spec.ResourcePolicy.ContainerPolicies[i]
		maxAllowed[policy.ContainerName] = policy.MaxAllowed

		recordMetric(containerMaxAllowed, labels, policy.ContainerName, "cpu", "core", policy.MaxAllowed.Cpu())
		recordMetric(containerMaxAllowed, labels, policy.ContainerName, "memory", "byte", policy.MaxAllowed.Memory())
	}

	for i := range vpa.Status.Recommendation.ContainerRecommendations {
		recommendation := vpa.Status.Recommendation.ContainerRecommendations[i]
		var maxRecommendation corev1.ResourceList
		if allowed, ok := maxAllowed["*"]; ok {
			maxRecommendation = allowed
		}
		if allowed, ok := maxAllowed[recommendation.ContainerName]; ok {
			maxRecommendation = allowed
		}
		if maxRecommendation == nil {
			continue
		}

		excess := substractResources(recommendation.UncappedTarget, maxRecommendation)
		recordMetric(containerRecommendationExcess, labels, recommendation.ContainerName, "cpu", "core", excess.Cpu())
		recordMetric(containerRecommendationExcess, labels, recommendation.ContainerName, "memory", "byte", excess.Memory())
	}
}

func substractResources(minuend, subtrahend corev1.ResourceList) corev1.ResourceList {
	result := make(corev1.ResourceList)
	for k, v := range minuend {
		if sub, ok := subtrahend[k]; ok {
			vCopy := v.DeepCopy()
			vCopy.Sub(sub)
			result[k] = vCopy
		}
	}
	return result
}

func recordMetric(gv *prometheus.GaugeVec, baseLabels prometheus.Labels, containerName, resourceName, unit string, q *resource.Quantity) {
	if q == nil {
		return
	}
	baseLabels["container"] = containerName
	baseLabels["resource"] = resourceName
	baseLabels["unit"] = unit
	gv.With(baseLabels).Set(q.AsApproximateFloat64())
}
