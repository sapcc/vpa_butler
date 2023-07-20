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
	var vpa = new(vpav1.VerticalPodAutoscaler)
	if err := v.Get(ctx, req.NamespacedName, vpa); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// clean-up vpa resources with old naming schema
	if !common.IsNewNamingSchema(vpa.GetName()) && common.IsHandleVPA(vpa) {
		err := v.Delete(ctx, vpa)
		if err != nil {
			return ctrl.Result{}, err
		}
		v.Log.Info("Cleanup old VPA successful", "namespace", vpa.GetNamespace(), "name", vpa.GetName())
		return ctrl.Result{}, nil
	}

	// update version annotation
	if common.IsHandleVPA(vpa) && v.Version != "" {
		version, ok := vpa.Annotations[common.AnnotationVPAButlerVersion]
		if !ok || version != v.Version {
			vpa.Annotations[common.AnnotationVPAButlerVersion] = v.Version
			err := v.Client.Update(ctx, vpa)
			if err != nil {
				return ctrl.Result{}, err
			}
			v.Log.Info("Updated version annotation", "namespace", vpa.GetNamespace(), "name", vpa.GetName())
		}
	}

	return ctrl.Result{}, nil
}
