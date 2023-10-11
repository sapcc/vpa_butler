package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	containerRecommendationExcess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vpa_butler_vpa_container_recommendation_excess",
		Help: "Subtracts the maximum allowed recommendation from the uncapped target recommendation per container",
	}, []string{"namespace", "verticalpodautoscaler", "container", "resource", "unit"})
)

func RegisterMetrics() {
	metrics.Registry.MustRegister(containerRecommendationExcess)
}

func RecordContainerRecommendationExcess(vpa *vpav1.VerticalPodAutoscaler) {
	// no policy => no maximum => no excess
	// no recommendations => no excess
	if vpa.Spec.ResourcePolicy == nil || vpa.Status.Recommendation == nil {
		return
	}
	maxAllowed := make(map[string]corev1.ResourceList)
	for i := range vpa.Spec.ResourcePolicy.ContainerPolicies {
		policy := vpa.Spec.ResourcePolicy.ContainerPolicies[i]
		maxAllowed[policy.ContainerName] = policy.MaxAllowed
	}
	for i := range vpa.Status.Recommendation.ContainerRecommendations {
		recommendation := vpa.Status.Recommendation.ContainerRecommendations[i]
		var maxRecommendation corev1.ResourceList
		if max, ok := maxAllowed["*"]; ok {
			maxRecommendation = max
		}
		if max, ok := maxAllowed[recommendation.ContainerName]; ok {
			maxRecommendation = max
		}
		if maxRecommendation == nil {
			continue
		}
		excess := substractResources(recommendation.UncappedTarget, maxRecommendation)
		labels := prometheus.Labels{
			"namespace":             vpa.Namespace,
			"verticalpodautoscaler": vpa.Name,
			"container":             recommendation.ContainerName,
		}
		if cpu := excess.Cpu(); cpu != nil {
			labels["resource"] = "cpu"
			labels["unit"] = "core"
			containerRecommendationExcess.With(labels).Set(cpu.AsApproximateFloat64())
		}
		if memory := excess.Memory(); memory != nil {
			labels["resource"] = "memory"
			labels["unit"] = "byte"
			containerRecommendationExcess.With(labels).Set(memory.AsApproximateFloat64())
		}
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
