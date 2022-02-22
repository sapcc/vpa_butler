package controllers

import (
	"context"
	"github.com/go-logr/logr"
	"github.com/sapcc/vpa_butler/internal/common"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"time"
)

type VPAStatefulSetController struct {
	client.Client
	Log logr.Logger
	Scheme *runtime.Scheme
	ReSyncPeriod time.Duration
}

func (v *VPAStatefulSetController) SetupWithManager (mgr ctrl.Manager) error {
	name := "statefulset-controller"
	v.Client = mgr.GetClient()
	v.Log = mgr.GetLogger().WithName(name)
	v.Scheme = mgr.GetScheme()
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&appsv1.StatefulSet{}).
		Watches(&source.Kind{Type: &vpav1.VerticalPodAutoscaler{}}, &handler.EnqueueRequestForObject{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 10, Log: v.Log}).
		Complete(v)
}

func (v *VPAStatefulSetController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	found, statefulSet, err := v.getStatefulSet(ctx, req)
	if err != nil {
		v.Log.Error(err, "error getting statefulset")
		return ctrl.Result{}, err
	}
	if ! found {
		// check for delete
		vpaFound, vpa, err := v.getVPA(ctx, req)
		if err != nil {
			v.Log.Error(err, "error getting vpa")
			return ctrl.Result{}, err
		}
		if vpaFound && vpa.Annotations["managedBy"] == "vpa_butler" && vpa.Annotations["vpa_controller"] == "VPAStatefulSetController" {
			v.Log.Info("delete vpa", "namespace", req.Namespace, "vpa", vpa.Name)
			err = v.Delete(ctx, vpa)
			if err != nil {
				v.Log.Error(err, "error deleting vpa")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}
	found, _, err = v.getVPA(ctx, req)
	if err != nil {
		v.Log.Error(err, "error getting vpa")
		return ctrl.Result{}, err
	}
	if found {
		// ignore existing vpa
		return ctrl.Result{}, nil
	}
	vpa := common.BuildVPA(statefulSet.Name, statefulSet.Namespace, statefulSet.Kind, statefulSet.APIVersion, "VPAStatefulSetController")
	v.Log.Info("create vpa", "namespace", req.Namespace, "statefulset", statefulSet.Name)
	err = v.Create(ctx, vpa)
	if err != nil {
		v.Log.Error(err, "error createing vpa")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (v *VPAStatefulSetController) getStatefulSet(ctx context.Context, req ctrl.Request) (bool, *appsv1.StatefulSet, error)  {
	statefulSet := new(appsv1.StatefulSet)
	err := v.Get(ctx, req.NamespacedName, statefulSet)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil, nil
		} else {
			return false, nil, err
		}
	}
	return true, statefulSet, nil
}

func (v *VPAStatefulSetController) getVPA(ctx context.Context, req ctrl.Request) (bool, *vpav1.VerticalPodAutoscaler, error) {
	vpa := new(vpav1.VerticalPodAutoscaler)
	err := v.Get(ctx, req.NamespacedName, vpa)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil, nil
		} else {
			return false, nil, err
		}
	}
	return true, vpa, nil
}
