package controllers

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/sapcc/vpa_butler/internal/common"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

const (
	controllerConcurrency = 10
)

type GenericController struct {
	client.Client
	typeName string
	Log      logr.Logger
	Scheme   *runtime.Scheme
	instance client.Object
}

func (v *GenericController) SetupWithManager(mgr ctrl.Manager, instance client.Object) error {
	v.typeName = reflect.TypeOf(instance).Elem().Name()
	name := fmt.Sprintf("%s-controller", v.typeName)
	v.Client = mgr.GetClient()
	v.Log = mgr.GetLogger().WithName(name)
	v.Scheme = mgr.GetScheme()
	v.instance = instance
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(instance).
		WithOptions(controller.Options{MaxConcurrentReconciles: controllerConcurrency}).
		Complete(v)
}

func (v *GenericController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	instance, ok := v.instance.DeepCopyObject().(client.Object)
	if !ok {
		return ctrl.Result{}, fmt.Errorf("failed to cast instance to client.Object")
	}
	if err := v.Get(ctx, req.NamespacedName, instance); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	result, err := common.ReconcileVPA(ctx, common.VPAReconcileParams{
		Client:   v.Client,
		Scheme:   v.Scheme,
		VpaOwner: instance,
		Log:      v.Log,
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	switch result {
	case common.OperationResultCreated, common.OperationResultUpdated:
		v.Log.Info(fmt.Sprintf("VPA for %s was %s", v.typeName, result), "namespace", req.Namespace, "name", req.Name)
	case common.OperationResultNone:
	}

	return ctrl.Result{}, nil
}

func SetupForAppsV1(mgr ctrl.Manager) error {
	deploymentController := GenericController{
		Client: mgr.GetClient(),
	}
	err := deploymentController.SetupWithManager(mgr, &appsv1.Deployment{})
	if err != nil {
		return fmt.Errorf("unable to setup deployment controller: %w", err)
	}
	daemonsetController := GenericController{
		Client: mgr.GetClient(),
	}
	err = daemonsetController.SetupWithManager(mgr, &appsv1.DaemonSet{})
	if err != nil {
		return fmt.Errorf("unable to setup daemonset controller: %w", err)
	}
	statefulSetController := GenericController{
		Client: mgr.GetClient(),
	}
	err = statefulSetController.SetupWithManager(mgr, &appsv1.StatefulSet{})
	if err != nil {
		return fmt.Errorf("unable to setup statefulset controller: %w", err)
	}
	return nil
}
