package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/sapcc/vpa_butler/internal/common"
	"k8s.io/apimachinery/pkg/runtime"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

type VPAController struct {
	client.Client
	Log     logr.Logger
	Scheme  *runtime.Scheme
	Version string
}

func (v *VPAController) SetupWithManager(mgr ctrl.Manager) error {
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

func (v *VPAController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	v.Log.Info("Reconciling VPA", "namespace", req.NamespacedName.Namespace, "name", req.NamespacedName.Name)
	var vpa = new(vpav1.VerticalPodAutoscaler)
	if err := v.Get(ctx, req.NamespacedName, vpa); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	abort, err := v.cleanupServedVPA(ctx, vpa)
	if err != nil {
		return ctrl.Result{}, err
	}
	if abort || !common.ManagedByButler(vpa) {
		return ctrl.Result{}, nil
	}
	if err := v.deleteOldVPA(ctx, vpa); err != nil {
		return ctrl.Result{}, err
	}

	// patch version annotation
	if v.Version != "" {
		version, ok := vpa.Annotations[common.AnnotationVPAButlerVersion]
		if !ok || version != v.Version {
			original := vpa.DeepCopy()
			vpa.Annotations[common.AnnotationVPAButlerVersion] = v.Version
			err := v.Client.Patch(ctx, vpa, client.MergeFrom(original))
			if err != nil {
				return ctrl.Result{}, err
			}
			v.Log.Info("Patched version annotation", "namespace", vpa.GetNamespace(), "name", vpa.GetName())
		}
	}

	return ctrl.Result{}, nil
}

// When there is a hand-crafted VPA targeting the same object as a served VPA the served one needs to be deleted.
// This functions returns true, when VPA currently being reconciled has been deleted.
func (v *VPAController) cleanupServedVPA(ctx context.Context, reconcileVPA *vpav1.VerticalPodAutoscaler) (bool, error) {
	v.Log.Info("Checking for deletion as a custom VPA was created",
		"namespace", reconcileVPA.GetNamespace(), "name", reconcileVPA.GetName())
	if reconcileVPA.Spec.TargetRef == nil {
		return false, nil
	}
	var vpas = new(vpav1.VerticalPodAutoscalerList)
	if err := v.List(ctx, vpas, client.InNamespace(reconcileVPA.GetNamespace())); err != nil {
		return false, err
	}
	// There are two cases to consider:
	// 1. The reconciled VPA is the served VPA.
	//    It gets deleted and we can early return as soon as any other VPA shares the same targetRef.
	// 2. The reconciled VPA is the hand-crafted VPA.
	//    If both VPAs compared within the are two different hand-crafted VPAs (which is still
	//    undefined behavior, but the butler does not care) no if applies and eventually the
	//    hand-crafted reconciled VPA is compared to the served one. It gets deleted and we can
	//    return early.
	for i := range vpas.Items {
		vpa := vpas.Items[i]
		if !common.EqualTarget(&vpa, reconcileVPA) || vpa.UID == reconcileVPA.UID {
			continue
		}
		if common.ManagedByButler(&vpa) {
			if err := v.Delete(ctx, &vpa); err != nil {
				return false, err
			}
			v.Log.Info("Deleted served VPA as a custom VPA was created",
				"namespace", vpa.GetNamespace(), "name", vpa.GetName())
			return false, nil
		}
		if common.ManagedByButler(reconcileVPA) {
			if err := v.Delete(ctx, reconcileVPA); err != nil {
				return false, err
			}
			v.Log.Info("Deleted served VPA as a custom VPA was created",
				"namespace", reconcileVPA.GetNamespace(), "name", reconcileVPA.GetName())
			return true, nil
		}
	}
	// When arriving here the cleanup the served VPA situation is sorted out.
	// No information about, whether the reconciled VPA is served or hand-crafted.
	return false, nil
}

// Clean-up vpa resources with old naming schema.
func (v *VPAController) deleteOldVPA(ctx context.Context, vpa *vpav1.VerticalPodAutoscaler) error {
	if !common.IsNewNamingSchema(vpa.GetName()) {
		err := v.Delete(ctx, vpa)
		if err != nil {
			return err
		}
		v.Log.Info("Cleanup old VPA successful", "namespace", vpa.GetNamespace(), "name", vpa.GetName())
	}
	return nil
}
