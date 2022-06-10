package controllers

import (
	"context"
	"fmt"
	"reflect"
	"time"

	porterv1 "get.porter.sh/operator/api/v1"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type PorterResource interface {
	client.Object
	GetStatus() porterv1.PorterResourceStatus
	SetStatus(value porterv1.PorterResourceStatus)
}

type patchFunc func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error

func PatchObjectWithRetry(ctx context.Context, log logr.Logger, clnt client.Client, patch patchFunc, obj client.Object, newObj func() client.Object) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	kind := obj.GetObjectKind().GroupVersionKind().Kind

	for {
		key := client.ObjectKeyFromObject(obj)
		latest := newObj()
		if err := clnt.Get(ctx, key, latest); err != nil {
			return errors.Wrap(err, fmt.Sprintf("could not get the latest %s definition", kind))
		}

		patchObj := client.MergeFrom(latest)
		err := patch(ctx, obj, patchObj)
		if err != nil {
			if apierrors.IsConflict(err) {
				continue // try again
			}
			return errors.Wrapf(err, "failed to patch %s", kind)
		}

		if log.V(Log4Debug).Enabled() {
			patchDump, _ := patchObj.Data(obj)
			log.V(Log4Debug).Info("Applied patch", "data", string(patchDump))
		}
		return nil
	}
}

func applyAgentAction(log logr.Logger, resource PorterResource, action *porterv1.AgentAction) {
	log.V(Log5Trace).Info(fmt.Sprintf("Syncing AgentAction status with %s", resource.GetObjectKind().GroupVersionKind().Kind))
	status := resource.GetStatus()
	status.ObservedGeneration = resource.GetGeneration()
	status.Phase = porterv1.PhaseUnknown

	if action == nil {
		status.Action = nil
		status.Conditions = nil
		log.V(Log5Trace).Info("Cleared status because there is no current agent action")
	} else {
		status.Action = &corev1.LocalObjectReference{Name: action.Name}
		if action.Status.Phase != "" {
			status.Phase = action.Status.Phase
		}
		status.Conditions = make([]metav1.Condition, len(action.Status.Conditions))
		copy(status.Conditions, action.Status.Conditions)

		if log.V(Log5Trace).Enabled() {
			conditions := make([]string, len(status.Conditions))
			for i, condition := range status.Conditions {
				conditions[i] = condition.Type
			}
			log.V(Log5Trace).Info("Copied status from agent action", "action", action.Name, "phase", action.Status.Phase, "conditions", conditions)
		}
	}

	resource.SetStatus(status)
}

// this is our kubectl delete check
func isDeleted(resource PorterResource) bool {
	timestamp := resource.GetDeletionTimestamp()
	return timestamp != nil && !timestamp.IsZero()
}

// ensure delete action is completed before delete
func isDeleteProcessed(resource PorterResource) bool {
	status := resource.GetStatus()
	return isDeleted(resource) && apimeta.IsStatusConditionTrue(status.Conditions, string(porterv1.ConditionComplete))
}

func isFinalizerSet(resource PorterResource) bool {
	for _, finalizer := range resource.GetFinalizers() {
		if finalizer == porterv1.FinalizerName {
			return true
		}
	}
	return false
}

// ensureFinalizerSet sets a finalizer on the resource and saves it, if necessary.
func ensureFinalizerSet(ctx context.Context, log logr.Logger, client client.Client, resource PorterResource) (updated bool, err error) {
	// Ensure all resources have a finalizer to we can react when they are deleted
	if !isDeleted(resource) {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !isFinalizerSet(resource) {
			log.V(Log5Trace).Info("adding finalizer")
			controllerutil.AddFinalizer(resource, porterv1.FinalizerName)
			return true, client.Update(ctx, resource)
		}
	}
	return false, nil
}

// removeFinalizer deletes the porter finalizer from the specified resource and saves it.
func removeFinalizer(ctx context.Context, log logr.Logger, client client.Client, inst *porterv1.Installation) error {
	log.V(Log5Trace).Info("removing finalizer")
	controllerutil.RemoveFinalizer(inst, porterv1.FinalizerName)
	return client.Update(ctx, inst)
}

// Build the set of labels used to uniquely identify the associated AgentAction.
func getActionLabels(resource metav1.Object) map[string]string {
	typeInfo, err := apimeta.TypeAccessor(resource)
	if err != nil {
		panic(err)
	}

	return map[string]string{
		porterv1.LabelManaged:            "true",
		porterv1.LabelResourceKind:       typeInfo.GetKind(),
		porterv1.LabelResourceName:       resource.GetName(),
		porterv1.LabelResourceGeneration: fmt.Sprintf("%d", resource.GetGeneration()),
	}
}

// resourceChanged is a predicate that filters events that are sent to Reconcile
// only triggers when the spec or the finalizer was changed.
// Allows forcing Reconcile with the retry annotation as well.
type resourceChanged struct {
	predicate.Funcs
}

func (resourceChanged) Update(e event.UpdateEvent) bool {
	if e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration() {
		return true
	}

	if !reflect.DeepEqual(e.ObjectNew.GetFinalizers(), e.ObjectOld.GetFinalizers()) {
		return true
	}

	if e.ObjectNew.GetAnnotations()[porterv1.AnnotationRetry] != e.ObjectOld.GetAnnotations()[porterv1.AnnotationRetry] {
		return true
	}

	return false
}
