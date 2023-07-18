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
		return OperationResultCreated, nil
	}

	// Return here if the butler does not manage this VPA.
	if !IsHandleVPA(vpa) {
		return OperationResultNone, nil
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
