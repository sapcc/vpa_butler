package common

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OperationResult is the action result of a CreateOrUpdate call.
type OperationResult string

const ( // They should complete the sentence "Deployment default/foo has been ..."
	// OperationResultNone means that the resource has not been changed.
	OperationResultNone OperationResult = "unchanged"
	// OperationResultCreated means that a new resource is created.
	OperationResultCreated OperationResult = "created"
	// OperationResultUpdated means that an existing resource is updated.
	OperationResultUpdated OperationResult = "updated"
	// OperationResultDeleted means that an existing resource is deleted.
	OperationResultDeleted OperationResult = "deleted"
	// maxNameLength is the maximum length of a VPA name.
	maxNameLength = 63
)

type VPAReconcileParams struct {
	Client   client.Client
	Scheme   *runtime.Scheme
	VpaOwner client.Object
	Log      logr.Logger
}

func ReconcileVPA(ctx context.Context, params VPAReconcileParams) (OperationResult, error) {
	params.Log.Info("Reconciling object",
		"namespace", params.VpaOwner.GetNamespace(),
		"name", params.VpaOwner.GetName(),
		"kind", params.VpaOwner.GetObjectKind().GroupVersionKind().Kind,
	)
	handle, err := ShouldHandleVPA(ctx, params)
	if err != nil {
		return OperationResultNone, err
	}
	if !handle {
		return deleteVPA(ctx, params)
	}

	var vpa = new(vpav1.VerticalPodAutoscaler)
	vpa.Namespace = params.VpaOwner.GetNamespace()
	vpa.Name = GetVPAName(params.VpaOwner)
	if err := params.Client.Get(ctx, client.ObjectKeyFromObject(vpa), vpa); err != nil {
		// Return any other error.
		if !apierrors.IsNotFound(err) {
			return OperationResultNone, err
		}
		// Mutate and create the VPA.
		if err := mutateVPA(params.Scheme, params.VpaOwner, vpa); err != nil {
			return OperationResultNone, errors.Wrap(err, "mutating object failed")
		}
		if err := params.Client.Create(ctx, vpa); err != nil {
			return OperationResultNone, ignoreAlreadyExists(err)
		}
		params.Log.Info("Created VPA", "uid", vpa.UID)
		return OperationResultCreated, nil
	}

	if o, err := meta.Accessor(vpa); err == nil {
		if o.GetDeletionTimestamp() != nil {
			return OperationResultNone, fmt.Errorf("the resource %s/%s already exists but is marked for deletion",
				o.GetNamespace(), o.GetName())
		}
	}
	return patch(ctx, vpa, params)
}

func patch(ctx context.Context, vpa *vpav1.VerticalPodAutoscaler, params VPAReconcileParams) (OperationResult, error) {
	before, ok := vpa.DeepCopyObject().(client.Object)
	if !ok {
		return OperationResultNone, fmt.Errorf("failed to cast object to client.Object")
	}
	if err := mutateVPA(params.Scheme, params.VpaOwner, vpa); err != nil {
		return OperationResultNone, errors.Wrap(err, "mutating object failed")
	}
	if equality.Semantic.DeepEqual(before, vpa) {
		return OperationResultNone, nil
	}
	patch := client.MergeFrom(before)
	if err := params.Client.Patch(ctx, vpa, patch); err != nil {
		return OperationResultNone, err
	}
	return OperationResultUpdated, nil
}

func deleteVPA(ctx context.Context, params VPAReconcileParams) (OperationResult, error) {
	var vpa vpav1.VerticalPodAutoscaler
	ref := types.NamespacedName{Namespace: params.VpaOwner.GetNamespace(), Name: GetVPAName(params.VpaOwner)}
	err := params.Client.Get(ctx, ref, &vpa)
	if apierrors.IsNotFound(err) {
		return OperationResultNone, nil
	} else if err != nil {
		return OperationResultNone, err
	}
	params.Log.Info("Deleting VPA as a hand-crafted VPA is already in place", "namespace", vpa.Namespace, "name", vpa.Name)
	err = params.Client.Delete(ctx, &vpa)
	return OperationResultDeleted, err
}

func ignoreAlreadyExists(err error) error {
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func GetVPAName(vpaOwner client.Object) string {
	name := vpaOwner.GetName()
	kind := strings.ToLower(vpaOwner.GetObjectKind().GroupVersionKind().Kind)
	if len(name)+len(kind) > maxNameLength {
		name = name[0 : len(name)-len(kind)-1]
	}
	return fmt.Sprintf("%s-%s", name, kind)
}

func IsNewNamingSchema(name string) bool {
	suffixes := []string{"-daemonset", "-statefulset", "-deployment"}
	for _, prefix := range suffixes {
		if strings.HasSuffix(name, prefix) {
			return true
		}
	}

	return false
}

func EqualTarget(a, b *vpav1.VerticalPodAutoscaler) bool {
	if a.Spec.TargetRef == nil || b.Spec.TargetRef == nil {
		return false
	}
	return a.Spec.TargetRef.Name == b.Spec.TargetRef.Name &&
		a.Spec.TargetRef.Kind == b.Spec.TargetRef.Kind &&
		a.Spec.TargetRef.APIVersion == b.Spec.TargetRef.APIVersion
}
