package controllers

import (
	"context"
	"strings"

	"github.com/go-logr/logr"
	"github.com/sapcc/vpa_butler/internal/common"
	"k8s.io/apimachinery/pkg/runtime"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

const (
	annotationVpaButlerVersion = "cloud.sap/vpa-butler-version"
)

type VpaController struct {
	client.Client
	Log     logr.Logger
	Scheme  *runtime.Scheme
	Version string
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
	v.Log.Info("Reconciling vpa", "namespace", req.NamespacedName.Namespace, "name", req.NamespacedName.Name)
	var vpa = new(vpav1.VerticalPodAutoscaler)
	if err := v.Get(ctx, req.NamespacedName, vpa); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	abort, err := v.cleanupServedVpa(ctx, vpa)
	if err != nil {
		return ctrl.Result{}, err
	}
	if abort || !common.ManagedByButler(vpa) {
		return ctrl.Result{}, nil
	}
	if err := v.deleteOldVpa(ctx, vpa); err != nil {
		return ctrl.Result{}, err
	}

	// patch version annotation
	if v.Version != "" {
		version, ok := vpa.Annotations[annotationVpaButlerVersion]
		if !ok || version != v.Version {
			original := vpa.DeepCopy()
			vpa.Annotations[annotationVpaButlerVersion] = v.Version
			err := v.Client.Patch(ctx, vpa, client.MergeFrom(original))
			if err != nil {
				return ctrl.Result{}, err
			}
			v.Log.Info("Patched version annotation", "namespace", vpa.GetNamespace(), "name", vpa.GetName())
		}
	}

	return ctrl.Result{}, nil
}

// When there is a hand-crafted vpa targeting the same object as a served vpa the served one needs to be deleted.
// This functions returns true, when vpa currently being reconciled has been deleted.
func (v *VpaController) cleanupServedVpa(ctx context.Context, reconcileVpa *vpav1.VerticalPodAutoscaler) (bool, error) {
	v.Log.Info("Checking for deletion as a custom vpa was created",
		"namespace", reconcileVpa.GetNamespace(), "name", reconcileVpa.GetName())
	if reconcileVpa.Spec.TargetRef == nil {
		return false, nil
	}
	var vpas = new(vpav1.VerticalPodAutoscalerList)
	if err := v.List(ctx, vpas, client.InNamespace(reconcileVpa.GetNamespace())); err != nil {
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
		if !equalTarget(&vpa, reconcileVpa) || vpa.UID == reconcileVpa.UID {
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
		if common.ManagedByButler(reconcileVpa) {
			if err := v.Delete(ctx, reconcileVpa); err != nil {
				return false, err
			}
			v.Log.Info("Deleted served vpa as a custom vpa was created",
				"namespace", reconcileVpa.GetNamespace(), "name", reconcileVpa.GetName())
			return true, nil
		}
	}
	// When arriving here the cleanup the served vpa situation is sorted out.
	// No information about, whether the reconciled vpa is served or hand-crafted.
	return false, nil
}

// Clean-up vpa resources with old naming schema.
func (v *VpaController) deleteOldVpa(ctx context.Context, vpa *vpav1.VerticalPodAutoscaler) error {
	if !isNewNamingSchema(vpa.GetName()) {
		err := v.Delete(ctx, vpa)
		if err != nil {
			return err
		}
		v.Log.Info("Cleanup old vpa successful", "namespace", vpa.GetNamespace(), "name", vpa.GetName())
	}
	return nil
}

func isNewNamingSchema(name string) bool {
	suffixes := []string{"-daemonset", "-statefulset", "-deployment"}
	for _, prefix := range suffixes {
		if strings.HasSuffix(name, prefix) {
			return true
		}
	}

	return false
}

func equalTarget(a, b *vpav1.VerticalPodAutoscaler) bool {
	if a.Spec.TargetRef == nil || b.Spec.TargetRef == nil {
		return false
	}
	return a.Spec.TargetRef.Name == b.Spec.TargetRef.Name &&
		a.Spec.TargetRef.Kind == b.Spec.TargetRef.Kind &&
		a.Spec.TargetRef.APIVersion == b.Spec.TargetRef.APIVersion
}
