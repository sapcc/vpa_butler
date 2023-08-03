package controllers

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"github.com/sapcc/vpa_butler/internal/common"
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

	v.Log.Info("Reconciling potential vpa target",
		"namespace", instance.GetNamespace(),
		"name", instance.GetName(),
		"kind", instance.GetObjectKind().GroupVersionKind().Kind,
	)
	serve, err := v.shouldServeVpa(ctx, instance)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !serve {
		err = v.ensureVpaDeleted(ctx, instance)
		return ctrl.Result{}, err
	}

	if v.reconcileVpa(ctx, instance) != nil {
		return ctrl.Result{}, err
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
			vpa.Spec.TargetRef.APIVersion == vpaOwner.GetObjectKind().GroupVersionKind().Version {
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

func (v *GenericController) reconcileVpa(ctx context.Context, vpaOwner client.Object) error {
	var vpa = new(vpav1.VerticalPodAutoscaler)
	vpa.Namespace = vpaOwner.GetNamespace()
	vpa.Name = getVpaName(vpaOwner)
	exists := true
	if err := v.Client.Get(ctx, client.ObjectKeyFromObject(vpa), vpa); err != nil {
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

	before, ok := vpa.DeepCopyObject().(client.Object)
	if !ok {
		return fmt.Errorf("failed to cast object to client.Object")
	}
	if err := configureVpa(v.Scheme, vpaOwner, vpa); err != nil {
		return errors.Wrap(err, "mutating object failed")
	}

	if !exists {
		v.Log.Info("Creating vpa", "name", vpa.Name, "namespace", vpa.Namespace)
		return v.Client.Create(ctx, vpa)
	}

	if equality.Semantic.DeepEqual(before, vpa) {
		return nil
	}
	patch := client.MergeFrom(before)
	v.Log.Info("Patching vpa", "name", vpa.Name, "namespace", vpa.Namespace)
	if err := v.Client.Patch(ctx, vpa, patch); err != nil {
		return err
	}
	return nil
}

func configureVpa(scheme *runtime.Scheme, vpaOwner client.Object, vpa *vpav1.VerticalPodAutoscaler) error {
	vpaSpec := &vpa.Spec
	vpaSpec.TargetRef = &autoscaling.CrossVersionObjectReference{
		Kind:       vpaOwner.GetObjectKind().GroupVersionKind().Kind,
		Name:       vpaOwner.GetName(),
		APIVersion: vpaOwner.GetObjectKind().GroupVersionKind().Version,
	}
	vpaSpec.UpdatePolicy = &vpav1.PodUpdatePolicy{
		UpdateMode: &common.VpaUpdateMode,
	}

	resourceList := []v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory}
	vpaSpec.ResourcePolicy = &vpav1.PodResourcePolicy{
		ContainerPolicies: []vpav1.ContainerResourcePolicy{
			{
				ContainerName:       "*",
				ControlledResources: &resourceList,
				ControlledValues:    &common.VpaControlledValues,
			},
		},
	}
	if vpa.Annotations == nil {
		vpa.Annotations = make(map[string]string, 0)
	}
	vpa.Annotations[common.AnnotationManagedBy] = common.AnnotationVpaButler

	return controllerutil.SetOwnerReference(vpaOwner, vpa, scheme)
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
