// Copyright 2024 SAP SE
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/sapcc/vpa_butler/internal/common"
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
	name := v.typeName + "-controller"
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
		return ctrl.Result{}, errors.New("failed to cast instance to client.Object")
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
	if err := v.Get(ctx, client.ObjectKeyFromObject(vpa), vpa); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		// vpa does not exist so create it
		// set off here, as the vpa is to be fully configured by the VpaController
		common.ConfigureVpaBaseline(vpa, instance, vpav1.UpdateModeOff)
		return ctrl.Result{}, v.Create(ctx, vpa)
	}
	return ctrl.Result{}, nil
}

func (v *GenericController) shouldServeVpa(ctx context.Context, vpaOwner client.Object) (bool, error) {
	ownerRefs := []autoscalingv1.CrossVersionObjectReference{{
		Name:       vpaOwner.GetName(),
		Kind:       vpaOwner.GetObjectKind().GroupVersionKind().Kind,
		APIVersion: vpaOwner.GetObjectKind().GroupVersionKind().GroupVersion().String(),
	}}
	owners := vpaOwner.GetOwnerReferences()
	for _, owner := range owners {
		ownerRefs = append(ownerRefs, autoscalingv1.CrossVersionObjectReference{
			Kind:       owner.Kind,
			Name:       owner.Name,
			APIVersion: owner.APIVersion,
		})
	}

	var vpas vpav1.VerticalPodAutoscalerList
	err := v.List(ctx, &vpas, client.InNamespace(vpaOwner.GetNamespace()))
	if err != nil {
		return false, fmt.Errorf("failed to list vpas: %w", err)
	}
	for i := range vpas.Items {
		vpa := vpas.Items[i]
		if vpa.Spec.TargetRef == nil || common.ManagedByButler(&vpa) {
			continue
		}
		for _, owner := range ownerRefs {
			// vpa matches the vpa owner
			if vpa.Spec.TargetRef.Name == owner.Name &&
				vpa.Spec.TargetRef.Kind == owner.Kind &&
				vpa.Spec.TargetRef.APIVersion == owner.APIVersion {
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
	err := v.Get(ctx, ref, &vpa)
	if apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}
	v.Log.Info("Deleting vpa as a hand-crafted vpa is already in place", "namespace", vpa.Namespace, "name", vpa.Name)
	return v.Delete(ctx, &vpa)
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
