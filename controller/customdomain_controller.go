package managed

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	operatoringressv1 "github.com/openshift/api/operatoringress/v1"
	customdomainv1alpha1 "github.com/openshift/custom-domains-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_customdomain")

// restrictedIngressNames contains an array of known managed ingresscontroller
var restrictedIngressNames = []string{"default", "apps2", "apps"}

// validObjectNames defines the format customdomains object names must adhere to. Derived from ingresscontroller objects, which require a DNS-1035 label
var validObjectNames = regexp.MustCompile("^[a-z]([-a-z0-9]*[a-z0-9])?$")

const (
	ingressNamespace         = "openshift-ingress"
	ingressOperatorNamespace = "openshift-ingress-operator"
	dnsConfigName            = "cluster"
	managedLabelName         = "customdomains.managed.openshift.io/managed"
	requeueWaitMinutes       = 1
	hostLength               = 6
	ingressDefaultScope      = "External"
	ELBIdleTimeoutDuration   = 1800
)

var IngressControllerELBIdleTimeout metav1.Duration = metav1.Duration{Duration: ELBIdleTimeoutDuration * time.Second}

// CustomDomainReconciler reconciles a CustomDomain object
type CustomDomainReconciler struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	Client client.Client
	Scheme *runtime.Scheme
}

const customDomainFinalizer = "finalizer.customdomain.managed.openshift.io"

//+kubebuilder:rbac:groups=managed.openshift.io,resources=customdomains,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=managed.openshift.io,resources=customdomains/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=managed.openshift.io,resources=customdomains/finalizers,verbs=update


// Reconcile reads that state of the cluster for a CustomDomain object and makes changes based on the state read
// and what is in the CustomDomain.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *CustomDomainReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling CustomDomain")

	// Fetch the CustomDomain instance
	instance := &customdomainv1alpha1.CustomDomain{}
	err := r.Client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if kerr.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Check if the CustomDomain instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isCustomDomainMarkedToBeDeleted := instance.GetDeletionTimestamp() != nil
	if isCustomDomainMarkedToBeDeleted {
		if contains(instance.GetFinalizers(), customDomainFinalizer) {
			// Run finalization logic for customDomainFinalizer. If the
			// finalization logic fails, don't remove the finalizer so
			// that we can retry during the next reconciliation.
			if err := r.finalizeCustomDomain(reqLogger, instance); err != nil {
				return reconcile.Result{}, err
			}

			// Remove customDomainFinalizer. Once all finalizers have been
			// removed, the object will be deleted.
			instance.SetFinalizers(remove(instance.GetFinalizers(), customDomainFinalizer))
			err := r.Client.Update(context.TODO(), instance)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	// Add finalizer for this CR
	if !contains(instance.GetFinalizers(), customDomainFinalizer) {
		if err := r.addFinalizer(reqLogger, instance); err != nil {
			return reconcile.Result{}, err
		}
	}

	// Check that the instance name does not clash with known managed names
	if contains(restrictedIngressNames, instance.Name) {
		errStr := fmt.Sprintf("Invalid CR name (%s)", instance.Name)
		reqLogger.Info(fmt.Sprintf("Instance name (%s) clashes with known names (%v)!", instance.Name, restrictedIngressNames))
		SetCustomDomainStatus(
			reqLogger,
			instance,
			errStr,
			customdomainv1alpha1.CustomDomainConditionInvalidName,
			customdomainv1alpha1.CustomDomainStateNotReady)
		_ = r.statusUpdate(reqLogger, instance)
		return reconcile.Result{}, errors.New(errStr)
	}

	// Check that the instance name is valid
	if !validObjectNames.Match([]byte(instance.Name)) {
		errStr := fmt.Sprintf("Invalid CR name (%s)", instance.Name)
		reqLogger.Info(fmt.Sprintf("Instance name (%s) does not conform to DNS guidelines: a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '%s')", instance.Name, validObjectNames.String()))
		SetCustomDomainStatus(
			reqLogger,
			instance,
			errStr,
			customdomainv1alpha1.CustomDomainConditionInvalidName,
			customdomainv1alpha1.CustomDomainStateNotReady)
		_ = r.statusUpdate(reqLogger, instance)
		return reconcile.Result{}, errors.New(errStr)
	}

	if instance.Status.State != customdomainv1alpha1.CustomDomainStateReady {
		// Update the status on CustomDomain
		SetCustomDomainStatus(
			reqLogger,
			instance,
			fmt.Sprintf("Creating Apps Custom Domain (%s)", instance.Spec.Domain),
			customdomainv1alpha1.CustomDomainConditionCreating,
			customdomainv1alpha1.CustomDomainStateNotReady)
		err := r.statusUpdate(reqLogger, instance)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// look up secret
	userSecret := &corev1.Secret{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: instance.Spec.Certificate.Namespace,
		Name:      instance.Spec.Certificate.Name,
	}, userSecret)
	if err != nil {
		reqLogger.Info(fmt.Sprintf("Error getting secret (%v)!", instance.Spec.Certificate.Name))
		// Update the status on CustomDomain
		SetCustomDomainStatus(
			reqLogger,
			instance,
			fmt.Sprintf("TLS Secret (%s) Not Found", instance.Spec.Certificate.Name),
			customdomainv1alpha1.CustomDomainConditionSecretNotFound,
			customdomainv1alpha1.CustomDomainStateNotReady)
		_ = r.statusUpdate(reqLogger, instance)
		return reconcile.Result{}, err
	}

	// add the CustomDomain's label to the secret for future monitoring
	_, labelFound := userSecret.Labels[managedLabelName]
	if !labelFound {
		reqLogger.Info(fmt.Sprintf("Adding label to the CustomDomain's secret (%s)", userSecret.Name))
		secretLabels := userSecret.GetLabels()
		if secretLabels == nil {
			secretLabels = make(map[string]string)
		}
		secretLabels[managedLabelName] = instance.Name
		userSecret.SetLabels(secretLabels)
		err = r.Client.Update(context.TODO(), userSecret)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Error updating labels for secret (%s)", userSecret.Name))
			return reconcile.Result{}, err
		}
	}

	// set the secret name to be the name of the customdomain instance
	secretName := instance.Name

	// create secret in the openshift-ingress namespace
	ingressSecret := &corev1.Secret{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: ingressNamespace,
		Name:      secretName,
	}, ingressSecret)
	if err != nil {
		if kerr.IsNotFound(err) {
			ingressSecret.Name = secretName
			ingressSecret.Namespace = ingressNamespace
			ingressSecret.Labels = labelsForOwnedResources()
			ingressSecret.Data = userSecret.Data
			ingressSecret.Type = userSecret.Type
			err = r.Client.Create(context.TODO(), ingressSecret)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Error creating custom certificate secret %s", secretName))
				return reconcile.Result{}, err
			}
		} else {
			reqLogger.Error(err, fmt.Sprintf("Error getting custom certificate secret %s", secretName))
			return reconcile.Result{}, err
		}
	} else {
		certificateUpdated := !reflect.DeepEqual(ingressSecret, userSecret)
		if certificateUpdated {
			reqLogger.Info("Secret change detected, updating certificate.")
			ingressSecret.Data = userSecret.Data
			err = r.Client.Update(context.TODO(), ingressSecret)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Error updating custom certificate secret %s", ingressSecret.Name))
				return reconcile.Result{}, err
			}
		} else {
			reqLogger.Info(fmt.Sprintf("Certificate secret %s already exists in the %s namespace", secretName, ingressNamespace))
		}
	}

	// get dnses.config.openshift.io/cluster for base domain
	dnsConfig := &configv1.DNS{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Name: dnsConfigName,
	}, dnsConfig)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Error getting dns.config/%s", dnsConfigName))
		return reconcile.Result{}, err
	}

	// set the ingress domain to be a subdomain under the cluster's installed basedomain
	// such that the record is added to the zone and external DNS can point to it
	ingressDomain := fmt.Sprintf("%s.%s", instance.Name, dnsConfig.Spec.BaseDomain)
	ingressName := instance.Name
	ingressScope := instance.Spec.Scope
	if ingressScope == "" {
		ingressScope = ingressDefaultScope
	}

	// create new ingresscontrollers.openshift.io
	customIngress := &operatorv1.IngressController{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: ingressOperatorNamespace,
		Name:      ingressName,
	}, customIngress)
	if err != nil {
		if kerr.IsNotFound(err) {
			customIngress.Name = ingressName
			customIngress.Namespace = ingressOperatorNamespace
			customIngress.Labels = labelsForOwnedResources()
			customIngress.Spec.Domain = ingressDomain
			customIngress.Spec.EndpointPublishingStrategy = &operatorv1.EndpointPublishingStrategy{
				Type: operatorv1.LoadBalancerServiceStrategyType,
				LoadBalancer: &operatorv1.LoadBalancerStrategy{
					Scope: operatorv1.LoadBalancerScope(ingressScope),
				},
			}

			cloudPlatform, err := GetPlatformType(r.Client)
			if err != nil {
				return reconcile.Result{}, err
			}
			isAWS := *cloudPlatform == "AWS"
			if isAWS {
				customIngress.Spec.EndpointPublishingStrategy.LoadBalancer.ProviderParameters = &operatorv1.ProviderLoadBalancerParameters{
					Type: operatorv1.AWSLoadBalancerProvider,
					AWS: &operatorv1.AWSLoadBalancerParameters{
						Type: "Classic",
						ClassicLoadBalancerParameters: &operatorv1.AWSClassicLoadBalancerParameters{
							ConnectionIdleTimeout: IngressControllerELBIdleTimeout,
						},
					},
				}
			}

			customIngress.Spec.NodePlacement = &operatorv1.NodePlacement{
				NodeSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"node-role.kubernetes.io/infra": ""},
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "node-role.kubernetes.io/infra",
						Effect:   corev1.TaintEffectNoSchedule,
						Operator: corev1.TolerationOpExists,
					},
				},
			}
			if customIngress.Spec.DefaultCertificate != nil {
				customIngress.Spec.DefaultCertificate.Name = secretName
			} else {
				customIngress.Spec.DefaultCertificate = &corev1.LocalObjectReference{Name: secretName}
			}
			err = r.Client.Create(context.TODO(), customIngress)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Error creating ingresscontroller %s in %s namespace", ingressName, ingressOperatorNamespace))
				return reconcile.Result{}, err
			}
		} else {
			reqLogger.Error(err, fmt.Sprintf("Error getting ingresscontroller %s in %s namespace", ingressName, ingressOperatorNamespace))
			return reconcile.Result{}, err
		}
	} else {
		// TODO: Check for scope change when customIngress.Spec.EndpointPublishingStrategy is nil
		if customIngress.Spec.EndpointPublishingStrategy != nil &&
			customIngress.Spec.EndpointPublishingStrategy.LoadBalancer != nil &&
			string(customIngress.Spec.EndpointPublishingStrategy.LoadBalancer.Scope) != ingressScope {
			errStr := fmt.Sprintf("Invalid update to ingress scope (detected change from %s to %s)", customIngress.Spec.EndpointPublishingStrategy.LoadBalancer.Scope, ingressScope)
			reqLogger.Info(fmt.Sprintf("The 'scope' field is immutable: detected change from %s to %s. To register a domain with %s scope, a new CustomDomain object will need to be defined.", customIngress.Spec.EndpointPublishingStrategy.LoadBalancer.Scope, ingressScope, ingressScope))
			SetCustomDomainStatus(
				reqLogger,
				instance,
				errStr,
				customdomainv1alpha1.CustomDomainConditionInvalidScope,
				customdomainv1alpha1.CustomDomainStateNotReady)
			_ = r.statusUpdate(reqLogger, instance)
			return reconcile.Result{}, errors.New(errStr)
		}
		reqLogger.Info(fmt.Sprintf("The ingresscontroller %s already exists in the %s namespace", ingressName, ingressOperatorNamespace))
	}

	// Obtain the dnsRecord to set in the CR status for final completion, requeue if not available
	dnsRecord := &operatoringressv1.DNSRecord{}
	dnsRecordName := fmt.Sprintf("%s-wildcard", instance.Name)
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Namespace: ingressOperatorNamespace,
		Name:      dnsRecordName,
	}, dnsRecord)
	if err != nil {
		if kerr.IsNotFound(err) {
			// requeue and wait for record
			return reconcile.Result{Requeue: true, RequeueAfter: time.Duration(requeueWaitMinutes) * time.Minute}, nil
		}
		return reconcile.Result{}, err
	}

	// Set the DNS record in the status from the actual DNS record created by ingress operator
	reqLogger.Info(fmt.Sprintf("DNSRecord %s created with value %s", dnsRecordName, dnsRecord.Spec.DNSName))
	instance.Status.DNSRecord = dnsRecord.Spec.DNSName

	// endpoint is a resolvable dns address w/ a random host under the ingress domain
	if len(instance.Status.Endpoint) == 0 {
		endpoint := fmt.Sprintf("%s.%s", randSeq(hostLength), ingressDomain)
		instance.Status.Endpoint = endpoint
	}

	// Update the status on CustomDomain
	SetCustomDomainStatus(
		reqLogger,
		instance,
		fmt.Sprintf("Custom Apps Domain (%s) Is Ready", instance.Spec.Domain),
		customdomainv1alpha1.CustomDomainConditionReady,
		customdomainv1alpha1.CustomDomainStateReady)
	err = r.statusUpdate(reqLogger, instance)
	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// labelsForOwnedResources creates a simple set of labels for all routes.
func labelsForOwnedResources() map[string]string {
	return map[string]string{managedLabelName: "true"}
}

// SetupWithManager sets up the controller with the Manager.
func (r *CustomDomainReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// secretSelectorPredicate filters the controller's reconcile events down to only Secrets that have the managedLabelName
	secretSelector := metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key: managedLabelName,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	}
	secretSelectorPredicate, err := predicate.LabelSelectorPredicate(secretSelector)
	if err != nil {
		return err
	}

	// secretHandler maps Secret reconcile events to the CustomDomain that utilizes the Secret
	secretHandler := handler.EnqueueRequestsFromMapFunc(func (obj client.Object) []reconcile.Request {
		customDomainName := obj.GetLabels()[managedLabelName]
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: customDomainName}}}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&customdomainv1alpha1.CustomDomain{}).
		Watches(&source.Kind{Type: &corev1.Secret{}},
			secretHandler,
			builder.WithPredicates(secretSelectorPredicate)).
		Complete(r)
}
