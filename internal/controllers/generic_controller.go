package controllers

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	"github.com/sapcc/vpa_butler/internal/common"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

const (
	controllerConcurrency = 10
	// maxNameLength is the maximum length of a vpa name.
	maxNameLength = 63
)

type GenericController struct {
	client.Client
	typeName string
	Log      logr.Logger
	Scheme   *runtime.Scheme
	instance client.Object
}

func (v *GenericController) SetupWithManager(mgr ctrl.Manager, instance client.Object) error {
	v.typeName = strings.ToLower(reflect.TypeOf(instance).Elem().Name())
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

	serve, err := v.shouldServeVpa(ctx, instance)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !serve {
		err = v.ensureVpaDeleted(ctx, instance)
		return ctrl.Result{}, err
	}
	v.Log.Info("Serving VPA for", "name", req.Name, "namespace", req.Namespace)
	var vpa = new(vpav1.VerticalPodAutoscaler)
	vpa.Namespace = instance.GetNamespace()
	vpa.Name = getVpaName(instance)
	if err := v.Client.Get(ctx, client.ObjectKeyFromObject(vpa), vpa); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		// vpa does not exist so create it
		// set off here, as the vpa is to be fully configured by the VpaController
		common.ConfigureVpaBaseline(vpa, instance, vpav1.UpdateModeOff)
		return ctrl.Result{}, v.Client.Create(ctx, vpa)
	}
	return ctrl.Result{}, nil
}

func (v *GenericController) shouldServeVpa(ctx context.Context, vpaOwner client.Object) (bool, error) {
	var vpas vpav1.VerticalPodAutoscalerList
	err := v.Client.List(ctx, &vpas, client.InNamespace(vpaOwner.GetNamespace()))
	if err != nil {
		return false, fmt.Errorf("failed to list vpas: %w", err)
	}
	for i := range vpas.Items {
		vpa := vpas.Items[i]
		if vpa.Spec.TargetRef == nil {
			continue
		}
		// vpa matches the vpa owner
		if vpa.Spec.TargetRef.Name == vpaOwner.GetName() &&
			vpa.Spec.TargetRef.Kind == vpaOwner.GetObjectKind().GroupVersionKind().Kind &&
			vpa.Spec.TargetRef.APIVersion == vpaOwner.GetObjectKind().GroupVersionKind().GroupVersion().String() {
			managed := common.ManagedByButler(&vpa)
			if !managed {
				// there is a hand-crafted vpa targeting a resource the butler cares about
				// so the served vpa needs to be deleted
				return false, nil
			}
		}
	}
	// no vpa found or vpa managed by butler, so handle it
	return true, nil
}

func (v *GenericController) ensureVpaDeleted(ctx context.Context, vpaOwner client.Object) error {
	var vpa vpav1.VerticalPodAutoscaler
	ref := types.NamespacedName{Namespace: vpaOwner.GetNamespace(), Name: getVpaName(vpaOwner)}
	err := v.Client.Get(ctx, ref, &vpa)
	if apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}
	v.Log.Info("Deleting vpa as a hand-crafted vpa is already in place", "namespace", vpa.Namespace, "name", vpa.Name)
	return v.Client.Delete(ctx, &vpa)
}

func getVpaName(vpaOwner client.Object) string {
	name := vpaOwner.GetName()
	kind := strings.ToLower(vpaOwner.GetObjectKind().GroupVersionKind().Kind)
	if len(name)+len(kind) > maxNameLength {
		name = name[0 : len(name)-len(kind)-1]
	}
	return fmt.Sprintf("%s-%s", name, kind)
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
