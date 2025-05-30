// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/sapcc/vpa_butler/internal/common"
	"github.com/sapcc/vpa_butler/internal/metrics"
)

const (
	annotationVpaButlerVersion = "cloud.sap/vpa-butler-version"
)

type VpaController struct {
	client.Client
	Log              logr.Logger
	Scheme           *runtime.Scheme
	MinAllowedCPU    resource.Quantity
	MinAllowedMemory resource.Quantity
	Version          string
}

func (v *VpaController) SetupWithManager(mgr ctrl.Manager) error {
	name := "vpa-controller"
	v.Client = mgr.GetClient()
	v.Log = mgr.GetLogger().WithName(name)
	v.Scheme = mgr.GetScheme()
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&vpav1.VerticalPodAutoscaler{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(v)
}

func (v *VpaController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	v.Log.Info("Reconciling vpa", "namespace", req.Namespace, "name", req.Name)
	var vpa = new(vpav1.VerticalPodAutoscaler)
	if err := v.Get(ctx, req.NamespacedName, vpa); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	deleted, err := v.deleteOrphanedVpa(ctx, vpa)
	if err != nil || deleted {
		return ctrl.Result{}, err
	}

	metrics.RecordContainerRecommendationExcess(vpa)
	target, err := v.extractTarget(ctx, vpa)
	if err != nil {
		return ctrl.Result{}, err
	}
	deleted, err = v.cleanupServedVpa(ctx, cleanupParams{vpa: vpa, target: target.object})
	if err != nil {
		return ctrl.Result{}, err
	}
	if deleted || !common.ManagedByButler(vpa) {
		return ctrl.Result{}, nil
	}
	deleted, err = v.deleteOldVpa(ctx, vpa)
	if err != nil || deleted {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, v.reconcileVpa(ctx, target)
}

type replicatedObject struct {
	object   client.Object
	replicas *int32
}

// Returns nil and no error, if the target kind is not considered by the vpa_butler.
func (v *VpaController) extractTarget(ctx context.Context, vpa *vpav1.VerticalPodAutoscaler) (replicatedObject, error) {
	if vpa.Spec.TargetRef == nil {
		return replicatedObject{}, fmt.Errorf("vpa %s/%s has nil target ref", vpa.Namespace, vpa.Name)
	}
	ref := *vpa.Spec.TargetRef
	switch ref.Kind {
	case DeploymentStr:
		var deployment appsv1.Deployment
		err := v.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: vpa.Namespace}, &deployment)
		if err != nil {
			return replicatedObject{}, fmt.Errorf("failed to fetch target %s/%s of kind %s for vpa",
				vpa.Namespace, ref.Name, ref.Kind)
		}
		return replicatedObject{object: &deployment, replicas: deployment.Spec.Replicas}, nil
	case StatefulSetStr:
		var sts appsv1.StatefulSet
		err := v.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: vpa.Namespace}, &sts)
		if err != nil {
			return replicatedObject{}, fmt.Errorf("failed to fetch target %s/%s of kind %s for vpa",
				vpa.Namespace, ref.Name, ref.Kind)
		}
		return replicatedObject{object: &sts, replicas: sts.Spec.Replicas}, nil
	case DaemonSetStr:
		var ds appsv1.DaemonSet
		err := v.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: vpa.Namespace}, &ds)
		if err != nil {
			return replicatedObject{}, fmt.Errorf("failed to fetch target %s/%s of kind %s for vpa",
				vpa.Namespace, ref.Name, ref.Kind)
		}
		return replicatedObject{object: &ds}, nil
	}
	v.Log.Info("unknown target kind", "kind", ref.Kind, "name", vpa.Name, "namespace", vpa.Namespace)
	return replicatedObject{}, nil
}

type cleanupParams struct {
	vpa    *vpav1.VerticalPodAutoscaler
	target client.Object
}

// When there is a hand-crafted vpa targeting the same object as a served vpa the served one needs to be deleted.
// This functions returns true, when vpa currently being reconciled has been deleted.
func (v *VpaController) cleanupServedVpa(ctx context.Context, params cleanupParams) (bool, error) {
	if params.vpa.Spec.TargetRef == nil {
		return false, nil
	}
	var vpas = new(vpav1.VerticalPodAutoscalerList)
	if err := v.List(ctx, vpas, client.InNamespace(params.vpa.GetNamespace())); err != nil {
		return false, err
	}
	// There are two cases to consider:
	// 1. The reconciled vpa is the served vpa.
	//    It gets deleted and we can early return as soon as any other vpa shares the same targetRef.
	// 2. The reconciled vpa is the hand-crafted vpa.
	//    If both vpas compared within the are two different hand-crafted vpas (which is still
	//    undefined behavior, but the butler does not care) no if applies and eventually the
	//    hand-crafted reconciled vpas is compared to the served one. It gets deleted and we can
	//    return early.
	for i := range vpas.Items {
		vpa := vpas.Items[i]
		if !equalTargetAcrossOwnerRefs(&vpa, params) {
			continue
		}
		if common.ManagedByButler(&vpa) {
			if err := v.Delete(ctx, &vpa); err != nil {
				return false, err
			}
			v.Log.Info("Deleted served vpa as a custom vpa was created",
				"namespace", vpa.GetNamespace(), "name", vpa.GetName())
			return false, nil
		}
		if common.ManagedByButler(params.vpa) {
			if err := v.Delete(ctx, params.vpa); err != nil {
				return false, err
			}
			v.Log.Info("Deleted served vpa as a custom vpa was created",
				"namespace", params.vpa.GetNamespace(), "name", params.vpa.GetName())
			return true, nil
		}
	}
	// When arriving here the cleanup the served vpa situation is sorted out.
	// No information about, whether the reconciled vpa is served or hand-crafted.
	return false, nil
}

// Clean-up vpa resources with old naming schema.
func (v *VpaController) deleteOldVpa(ctx context.Context, vpa *vpav1.VerticalPodAutoscaler) (bool, error) {
	if !isNewNamingSchema(vpa.GetName()) {
		err := v.Delete(ctx, vpa)
		if err != nil {
			return false, err
		}
		v.Log.Info("Cleanup old vpa successful", "namespace", vpa.GetNamespace(), "name", vpa.GetName())
		return true, nil
	}
	return false, nil
}

// Cleanup-up served Vpas, which target have been removed.
// Compared to finalizers on the targets (deployments,...) this approach is more
// lazy as the vpa needs to be reconciled, but it does not put finalizers on critical resources.
func (v *VpaController) deleteOrphanedVpa(ctx context.Context, vpa *vpav1.VerticalPodAutoscaler) (bool, error) {
	if !common.ManagedByButler(vpa) {
		return false, nil
	}
	if vpa.Spec.TargetRef == nil {
		v.Log.Info("Deleting Vpa with orphaned target")
		return true, v.Delete(ctx, vpa)
	}
	name := types.NamespacedName{Namespace: vpa.Namespace, Name: vpa.Spec.TargetRef.Name}
	var obj client.Object
	switch vpa.Spec.TargetRef.Kind {
	case DeploymentStr:
		obj = &appsv1.Deployment{}
	case StatefulSetStr:
		obj = &appsv1.StatefulSet{}
	case DaemonSetStr:
		obj = &appsv1.DaemonSet{}
	}
	err := v.Get(ctx, name, obj)
	if apierrors.IsNotFound(err) {
		v.Log.Info("Deleting Vpa with orphaned target")
		return true, v.Delete(ctx, vpa)
	}
	return false, err
}

func (v *VpaController) reconcileVpa(ctx context.Context, vpaOwner replicatedObject) error {
	var vpa = new(vpav1.VerticalPodAutoscaler)
	vpa.Namespace = vpaOwner.object.GetNamespace()
	vpa.Name = getVpaName(vpaOwner.object)
	exists := true
	if err := v.Get(ctx, client.ObjectKeyFromObject(vpa), vpa); err != nil {
		// Return any other error.
		if !apierrors.IsNotFound(err) {
			return err
		}
		exists = false
	}

	if o, err := meta.Accessor(vpa); err == nil {
		if o.GetDeletionTimestamp() != nil {
			return fmt.Errorf("the resource %s/%s already exists but is marked for deletion",
				o.GetNamespace(), o.GetName())
		}
	}

	before := vpa.DeepCopy()
	if err := v.configureVpa(vpaOwner, vpa); err != nil {
		return errors.Wrap(err, "mutating object failed")
	}

	if !exists {
		v.Log.Info("Creating vpa", "name", vpa.Name, "namespace", vpa.Namespace)
		return v.Create(ctx, vpa)
	}

	if equality.Semantic.DeepEqual(before, vpa) {
		return nil
	}
	patch := client.MergeFrom(before)
	v.Log.Info("Patching vpa", "name", vpa.Name, "namespace", vpa.Namespace)
	if err := v.Patch(ctx, vpa, patch); err != nil {
		return err
	}
	return nil
}

func (v *VpaController) configureVpa(vpaOwner replicatedObject, vpa *vpav1.VerticalPodAutoscaler) error {
	common.ConfigureVpaBaseline(vpa, vpaOwner.object, common.VpaUpdateMode)
	annotations := vpaOwner.object.GetAnnotations()

	if updateModeStr, ok := annotations[UpdateModeAnnotationKey]; ok {
		if slices.Contains(common.SupportedUpdatedModes, updateModeStr) {
			updateMode := vpav1.UpdateMode(updateModeStr)
			vpa.Spec.UpdatePolicy.UpdateMode = &updateMode
		}
	}

	vpa.Spec.UpdatePolicy.MinReplicas = nil
	if vpa.Spec.UpdatePolicy.UpdateMode != nil {
		autoModes := []vpav1.UpdateMode{vpav1.UpdateModeAuto, vpav1.UpdateModeRecreate}
		if slices.Contains(autoModes, *vpa.Spec.UpdatePolicy.UpdateMode) {
			if vpaOwner.replicas != nil && *vpaOwner.replicas <= 1 {
				vpa.Spec.UpdatePolicy.MinReplicas = ptr.To(int32(1))
			}
		}
	}

	ctrlValues := common.VpaControlledValues
	if ctrlValuesStr, ok := annotations[ControlledValuesAnnotationKey]; ok {
		if slices.Contains(common.SupportedControlledValues, ctrlValuesStr) {
			ctrlValues = vpav1.ContainerControlledValues(ctrlValuesStr)
		}
	}

	resourceList := []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory}
	if vpa.Spec.ResourcePolicy == nil || len(vpa.Spec.ResourcePolicy.ContainerPolicies) == 0 {
		containerResourcePolicy := vpav1.ContainerResourcePolicy{
			ContainerName:       "*",
			ControlledResources: &resourceList,
			ControlledValues:    &ctrlValues,
			MinAllowed: corev1.ResourceList{
				corev1.ResourceCPU:    v.MinAllowedCPU,
				corev1.ResourceMemory: v.MinAllowedMemory,
			},
		}
		vpa.Spec.ResourcePolicy = &vpav1.PodResourcePolicy{
			ContainerPolicies: []vpav1.ContainerResourcePolicy{containerResourcePolicy},
		}
	} else {
		for i := range vpa.Spec.ResourcePolicy.ContainerPolicies {
			current := &vpa.Spec.ResourcePolicy.ContainerPolicies[i]
			current.ControlledResources = &resourceList
			current.ControlledValues = &ctrlValues
			current.MinAllowed = corev1.ResourceList{
				corev1.ResourceCPU:    v.MinAllowedCPU,
				corev1.ResourceMemory: v.MinAllowedMemory,
			}
		}
	}
	vpa.Annotations[annotationVpaButlerVersion] = v.Version

	return controllerutil.SetOwnerReference(vpaOwner.object, vpa, v.Scheme)
}

func isNewNamingSchema(name string) bool {
	suffixes := []string{"-daemonset", "-statefulset", "-deployment"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}

	return false
}

func equalTargetAcrossOwnerRefs(vpa *vpav1.VerticalPodAutoscaler, params cleanupParams) bool {
	if equalTarget(vpa.Spec.TargetRef, params.vpa.Spec.TargetRef) && vpa.UID != params.vpa.UID {
		return true
	}
	if params.target == nil {
		return false
	}
	sameTarget := false
	for _, owner := range params.target.GetOwnerReferences() {
		crossRef := &autoscalingv1.CrossVersionObjectReference{
			Kind:       owner.Kind,
			Name:       owner.Name,
			APIVersion: owner.APIVersion,
		}
		if equalTarget(vpa.Spec.TargetRef, crossRef) && vpa.UID != params.vpa.UID {
			sameTarget = true
		}
	}
	return sameTarget
}

func equalTarget(a, b *autoscalingv1.CrossVersionObjectReference) bool {
	if a == nil || b == nil {
		return false
	}
	// apparently the apiVersion is currently not considered by the
	// vpa so v1 and apps/v1 work for deployments etc., so ignore
	// the prefix if only one apiVersion has a prefix
	apiEqual := false
	aSplitted := strings.Split(a.APIVersion, "/")
	bSplitted := strings.Split(b.APIVersion, "/")
	if len(aSplitted) == len(bSplitted) {
		apiEqual = a.APIVersion == b.APIVersion
	} else {
		apiEqual = aSplitted[len(aSplitted)-1] == bSplitted[len(bSplitted)-1]
	}

	return a.Name == b.Name &&
		a.Kind == b.Kind &&
		apiEqual
}
