package managed

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"

	"github.com/go-logr/logr"
	compare "github.com/hashicorp/go-version"
	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	customdomainv1alpha1 "github.com/openshift/custom-domains-operator/api/v1alpha1"
	"github.com/openshift/custom-domains-operator/config"
	corev1 "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	legacyIngressSupportLabel = "ext-managed.openshift.io/legacy-ingress-support"
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

// Take an ingress controller managed by the custom domains operator and release it back to the
// cluster ingress operator. Also schedule it onto customer worker nodes from the Red Hat managed infra
// nodes.
func (r *CustomDomainReconciler) returnIngressToClusterIngressOperator(reqLogger logr.Logger, instance *customdomainv1alpha1.CustomDomain) (ctrl.Result, error) {
	reqLogger.Info(fmt.Sprintf("Removing operator management labels from %s's underlying ingress controller", instance.Name))

	ingressName := instance.Name
	customIngress := &operatorv1.IngressController{}

	reqLogger.Info(fmt.Sprintf("Fetching ingress controller: %s/%s", ingressOperatorNamespace, ingressName))
	err := r.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: ingressOperatorNamespace,
		Name:      ingressName,
	}, customIngress)

	if err != nil {
		if kerr.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Error(err, fmt.Sprintf("Ingresscontroller %s in %s namespace not found", ingressName, ingressOperatorNamespace))
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	delete(customIngress.Labels, managedLabelName)
	customIngress.Spec.NodePlacement = &operatorv1.NodePlacement{
		NodeSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""},
		},
		Tolerations: []corev1.Toleration{},
	}

	reqLogger.Info(fmt.Sprintf("Updating ingress %s with new node placement on worker node, removing tolerations for infra nodes", instance.Name))
	err = r.Client.Update(context.TODO(), customIngress)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Error updating ingresscontroller %s in %s namespace", ingressName, ingressOperatorNamespace))
		return reconcile.Result{}, err
	}

	userSecret := &corev1.Secret{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: instance.Spec.Certificate.Namespace,
		Name:      instance.Spec.Certificate.Name,
	}, userSecret)
	reqLogger.Info(fmt.Sprintf("Updating secret to remove custom domain labels from secret %s", userSecret.Name))
	delete(userSecret.Labels, managedLabelName)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Error fetching secret %s in %s namespace", userSecret.Name, userSecret.Namespace))
		return reconcile.Result{}, err
	}

	err = r.Client.Update(context.TODO(), userSecret)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Error updating secret %s in %s namespace", userSecret.Name, userSecret.Namespace))
		// Requeue, as the dependent ingress controller has already been updated
		return reconcile.Result{}, err
	}

	SetCustomDomainStatus(
		reqLogger,
		instance,
		"Due to the deprecation of the custom domains operator on OSD/ROSA version 4.13 and above, this CustomDomain no longer manages an IngressController.",
		customdomainv1alpha1.CustomDomainConditionDeprecated,
		customdomainv1alpha1.CustomDomainStateNotReady)
	err = r.statusUpdate(reqLogger, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
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
		// #nosec G404
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// GetPlatformType returns the cloud platform type for the cluster
func GetPlatformType(kclient client.Client) (*configv1.PlatformType, error) {
	infra, err := GetInfrastructureObject(kclient)
	if err != nil {
		return nil, err
	}
	return &infra.Status.PlatformStatus.Type, nil
}

// GetInfrastructureObject returns the canonical Infrastructure object
func GetInfrastructureObject(kclient client.Client) (*configv1.Infrastructure, error) {
	infrastructure := &configv1.Infrastructure{}
	if err := kclient.Get(context.TODO(), client.ObjectKey{Name: "cluster"}, infrastructure); err != nil {
		return nil, fmt.Errorf("failed to get default infrastructure with name cluster: %w", err)
	}

	return infrastructure, nil
}

// Taken from https://github.com/openshift/cloud-ingress-operator/blob/master/pkg/utils/clusterversion.go
// GetClusterVersionObject returns the canonical ClusterVersion object
// To check current version: `output.Status.History[0].Version`
//
// `history contains a list of the most recent versions applied to the cluster.
// This value may be empty during cluster startup, and then will be updated when a new update is being applied.
// The newest update is first in the list and it is ordered by recency`
func (r *CustomDomainReconciler) GetClusterVersion(kclient client.Client) (string, error) {
	versionObject := &configv1.ClusterVersion{}
	ns := types.NamespacedName{
		Namespace: "",
		Name:      "version",
	}
	err := kclient.Get(context.TODO(), ns, versionObject)
	if err != nil {
		return "", err
	}

	// handle when there's no object defined || no version found on history
	if versionObject == nil || len(versionObject.Status.History) == 0 {
		return "", fmt.Errorf("version couldn't be grabbed from clusterversion: %+v", versionObject) // (%+v) adds field names
	}

	return versionObject.Status.History[0].Version, nil
}

func isUsingNewManagedIngressFeature(kclient client.Client, reqLogger logr.Logger) (bool, error) {
	reqLogger.Info("Fetching labels from namespace", "namespace", config.OperatorNamespace)
	ns := corev1.Namespace{}
	if err := kclient.Get(context.TODO(), client.ObjectKey{Name: config.OperatorNamespace}, &ns); err != nil {
		return false, err
	}

	labels := ns.Labels

	return labels[legacyIngressSupportLabel] == "false", nil
}

// IsVersionGreaterOrEqualThan compares major and minor versions of a and b
// For example, if (4.10.1, 4.10.0) is supplied, this returns true because 4.10 >= 4.10
// anything other than major and minor are ignored when comparing the versions.
func IsVersionGreaterOrEqualThan(a, b string) bool {
	// Convert versions into ${major}.${minor}, for example 4.10.0-rc.4 to 4.10
	re := regexp.MustCompile("([0-9]+).([0-9]+)([0-9]?)")
	shortVersion := re.FindString(a)

	aVersion, err := compare.NewVersion(shortVersion)
	if err != nil {
		return false
	}

	bVersion, err := compare.NewVersion(b)
	if err != nil {
		return false
	}

	return !aVersion.LessThan(bVersion)
}
