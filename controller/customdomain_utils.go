package managed

import (
	"context"
	"fmt"
	"math/rand"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	customdomainv1alpha1 "github.com/openshift/custom-domains-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// UpdateConditionCheck tests whether a condition should be updated from the
// old condition to the new condition. Returns true if the condition should
// be updated.
type UpdateConditionCheck func(oldReason, oldMessage, newReason, newMessage string) bool

// UpdateConditionAlways returns true. The condition will always be updated.
func UpdateConditionAlways(_, _, _, _ string) bool {
	return true
}

// UpdateConditionNever return false. The condition will never be updated,
// unless there is a change in the status of the condition.
func UpdateConditionNever(_, _, _, _ string) bool {
	return false
}

// UpdateConditionIfReasonOrMessageChange returns true if there is a change
// in the reason or the message of the condition.
func UpdateConditionIfReasonOrMessageChange(oldReason, oldMessage, newReason, newMessage string) bool {
	return oldReason != newReason ||
		oldMessage != newMessage
}

// ShouldUpdateCondition returns true if condition needs update
func ShouldUpdateCondition(
	oldStatus corev1.ConditionStatus, oldReason, oldMessage string,
	newStatus corev1.ConditionStatus, newReason, newMessage string,
	updateConditionCheck UpdateConditionCheck,
) bool {
	if oldStatus != newStatus {
		return true
	}
	return updateConditionCheck(oldReason, oldMessage, newReason, newMessage)
}

// SetCustomDomainCondition sets a condition on a CustomDomain resource's status
func SetCustomDomainCondition(
	conditions []customdomainv1alpha1.CustomDomainCondition,
	conditionType customdomainv1alpha1.CustomDomainConditionType,
	status corev1.ConditionStatus,
	message string,
	updateConditionCheck UpdateConditionCheck,
) []customdomainv1alpha1.CustomDomainCondition {
	now := metav1.Now()
	existingCondition := FindCustomDomainCondition(conditions, conditionType)
	if existingCondition == nil {
		if status == corev1.ConditionTrue {
			conditions = append(
				conditions,
				customdomainv1alpha1.CustomDomainCondition{
					Type:               conditionType,
					Status:             status,
					Reason:             string(conditionType),
					Message:            message,
					LastTransitionTime: now,
					LastProbeTime:      now,
				},
			)
		}
	} else {
		if ShouldUpdateCondition(
			existingCondition.Status, existingCondition.Reason, existingCondition.Message,
			status, string(conditionType), message,
			updateConditionCheck,
		) {
			if existingCondition.Status != status {
				existingCondition.LastTransitionTime = now
			}
			existingCondition.Status = status
			existingCondition.Reason = string(conditionType)
			existingCondition.Message = message
			existingCondition.LastProbeTime = now
		}
	}
	return conditions
}

// FindCustomDomainCondition finds in the condition that has the
// specified condition type in the given list. If none exists, then returns nil.
func FindCustomDomainCondition(conditions []customdomainv1alpha1.CustomDomainCondition, conditionType customdomainv1alpha1.CustomDomainConditionType) *customdomainv1alpha1.CustomDomainCondition {
	for i, condition := range conditions {
		if condition.Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

// finalizeCustomDomain cleans up left over resources once a CustomDomain CR is deleted
func (r *CustomDomainReconciler) finalizeCustomDomain(reqLogger logr.Logger, instance *customdomainv1alpha1.CustomDomain) error {
	reqLogger.Info("Deleting old resources...")
	// get and delete the secret in openshift-ingress
	ingressSecret := &corev1.Secret{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: ingressNamespace,
		Name:      instance.Name,
	}, ingressSecret)
	if err != nil {
		if !kerr.IsNotFound(err) {
			reqLogger.Error(err, fmt.Sprintf("Failed to get %s secret", instance.Name))
			return err
		}
		reqLogger.Info(fmt.Sprintf("Secret %s was not found, skipping.", instance.Name))
	} else {
		if _, ok := ingressSecret.Labels[managedLabelName]; ok {
			err = r.Client.Delete(context.TODO(), ingressSecret)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Failed to delete %s secret", instance.Name))
				return err
			}
		} else {
			reqLogger.Info(fmt.Sprintf("Secret %s did not have proper labels, skipping.", ingressSecret.Name))
		}
	}

	// get and delete the custom ingresscontroller
	customIngress := &operatorv1.IngressController{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: ingressOperatorNamespace,
		Name:      instance.Name,
	}, customIngress)
	if err != nil {
		if !kerr.IsNotFound(err) {
			reqLogger.Error(err, fmt.Sprintf("Failed to get %s ingresscontroller", instance.Name))
			return err
		}
		reqLogger.Info(fmt.Sprintf("IngressController %s was not found, skipping.", instance.Name))
	} else {
		// Only delete the IngressController if it has the proper labels and does not have a restricted name
		if _, ok := customIngress.Labels[managedLabelName]; ok {
			if !contains(restrictedIngressNames, customIngress.Name) {
				err = r.Client.Delete(context.TODO(), customIngress)
				if err != nil {
					reqLogger.Error(err, fmt.Sprintf("Failed to delete %s ingresscontroller", customIngress.Name))
					return err
				}
			} else {
				reqLogger.Info(fmt.Sprintf("IngressController %s has a restricted name, not deleting.", customIngress.Name))
			}
		} else {
			reqLogger.Info(fmt.Sprintf("IngressController %s did not have proper labels, not deleting.", customIngress.Name))
		}
	}
	reqLogger.Info(fmt.Sprintf("Customdomain %s successfully finalized", instance.Name))
	return nil
}

// addFinalizer is a function that adds a finalizer for the CustomDomain CR
func (r *CustomDomainReconciler) addFinalizer(reqLogger logr.Logger, m *customdomainv1alpha1.CustomDomain) error {
	reqLogger.Info("Adding Finalizer for the CustomDomain")
	m.SetFinalizers(append(m.GetFinalizers(), customDomainFinalizer))

	// Update CR
	err := r.Client.Update(context.TODO(), m)
	if err != nil {
		reqLogger.Error(err, "Failed to update CustomDomain with finalizer")
		return err
	}
	return nil
}

// SetCustomDomainStatus sets the status of the custom domain resource
func SetCustomDomainStatus(reqLogger logr.Logger, instance *customdomainv1alpha1.CustomDomain, message string, condition customdomainv1alpha1.CustomDomainConditionType, state customdomainv1alpha1.CustomDomainStateType) {
	instance.Status.Conditions = SetCustomDomainCondition(
		instance.Status.Conditions,
		condition,
		corev1.ConditionTrue,
		message,
		UpdateConditionNever)
	instance.Status.State = state
	reqLogger.Info(fmt.Sprintf("CustomDomain (%s) status updated: condition: (%s), state: (%s)", instance.Name, string(condition), string(state)))
}

// statusUpdate helper function to set the actual status update
func (r *CustomDomainReconciler) statusUpdate(reqLogger logr.Logger, instance *customdomainv1alpha1.CustomDomain) error {
	err := r.Client.Status().Update(context.TODO(), instance)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Status update for %s failed", instance.Name))
	}
	//reqLogger.Info(fmt.Sprintf("Status updated for %s", instance.Name))
	return err
}

// contains is a helper function for finding a string in an array
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// remove is a helper function for finalizer
func remove(list []string, s string) []string {
	for i, v := range list {
		if v == s {
			list = append(list[:i], list[i+1:]...)
		}
	}
	return list
}

// letters used by randSeq
var letters = []rune("abcdefghijklmnopqrstuvwxyz")

// randSeq is a function to generate a fixed length string with random letters
func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
