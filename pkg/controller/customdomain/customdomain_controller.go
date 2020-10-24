package customdomain

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	customdomainv1alpha1 "github.com/openshift/custom-domains-operator/pkg/apis/customdomain/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_customdomain")

const (
	ingressNamespace         = "openshift-ingress"
	ingressOperatorNamespace = "openshift-ingress-operator"
)

// Add creates a new CustomDomain Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileCustomDomain{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("customdomain-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource CustomDomain
	err = c.Watch(&source.Kind{Type: &customdomainv1alpha1.CustomDomain{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(drow): Add more watches to default LB service and watch for changes.
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &customdomainv1alpha1.CustomDomain{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileCustomDomain implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileCustomDomain{}

// ReconcileCustomDomain reconciles a CustomDomain object
type ReconcileCustomDomain struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

const customDomainFinalizer = "finalizer.customdomain.managed.openshift.io"

// Reconcile reads that state of the cluster for a CustomDomain object and makes changes based on the state read
// and what is in the CustomDomain.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileCustomDomain) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling CustomDomain")

	// Fetch the CustomDomain instance
	instance := &customdomainv1alpha1.CustomDomain{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
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
			err := r.client.Update(context.TODO(), instance)
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

	// finally modify the custom domain
	if err := createOrAddCustomAppsDomain(r, reqLogger, instance); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// createOrAddCustomAppsDomain creates the apps custom domain
func createOrAddCustomAppsDomain(r *ReconcileCustomDomain, reqLogger logr.Logger, instance *customdomainv1alpha1.CustomDomain) error {
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
			return err
		}
	}

	// look up secret
	userSecret := &corev1.Secret{}
	err := r.client.Get(context.TODO(), types.NamespacedName{
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
		return err
	}

	// set the secret name to be the name of the customdomain instance
	secretName := instance.Name

	// create or update secret in openshift-ingress
	ingressSecret := &corev1.Secret{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Namespace: ingressNamespace,
		Name:      secretName,
	}, ingressSecret)
	ingressSecret.Name = secretName
	ingressSecret.Namespace = ingressNamespace
	ingressSecret.Data = userSecret.Data
	ingressSecret.Type = userSecret.Type
	if err != nil {
		err = r.client.Create(context.TODO(), ingressSecret)
		if err != nil {
			reqLogger.Error(err, "Error creating custom certificate secret")
			return err
		}
	} else {
		err = r.client.Update(context.TODO(), ingressSecret)
		if err != nil {
			reqLogger.Error(err, "Error updating custom certificate secret")
			return err
		}
	}

	// get dnses.config.openshift.io/cluster for installed base domain
	dnsConfig := &configv1.DNS{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name: "cluster",
	}, dnsConfig)
	if err != nil {
		reqLogger.Error(err, "Error getting dns.config/cluster")
		return err
	}

	// set the ingress domain to be a subdomain under the cluster's installed basedomain
	// such that the record is added to the zone and external DNS can point to it
	ingressDomain := fmt.Sprintf("%s.%s", instance.Name, dnsConfig.Spec.BaseDomain)
	ingressName := instance.Name

	// create or update ingresscontrollers.openshift.io/custom
	customIngress := &operatorv1.IngressController{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Namespace: ingressOperatorNamespace,
		Name:      ingressName,
	}, customIngress)
	customIngress.Name = ingressName
	customIngress.Namespace = ingressOperatorNamespace
	customIngress.Spec.Domain = ingressDomain
	if customIngress.Spec.DefaultCertificate != nil {
		customIngress.Spec.DefaultCertificate.Name = secretName
	} else {
		customIngress.Spec.DefaultCertificate = &corev1.LocalObjectReference{Name: secretName}
	}
	if err != nil {
		err = r.client.Create(context.TODO(), customIngress)
		if err != nil {
			reqLogger.Error(err, "Error creating custom ingress controller")
			return err
		}
	} else {
		err = r.client.Update(context.TODO(), customIngress)
		if err != nil {
			reqLogger.Error(err, "Error updating custom ingress controller")
			return err
		}
	}
	// Set the DNS record in the status
	instance.Status.DNSRecord = fmt.Sprintf("*.%s", ingressDomain)

	// Update the status on CustomDomain
	SetCustomDomainStatus(
		reqLogger,
		instance,
		fmt.Sprintf("Custom Apps Domain (%s) Is Ready", instance.Spec.Domain),
		customdomainv1alpha1.CustomDomainConditionReady,
		customdomainv1alpha1.CustomDomainStateReady)
	err = r.statusUpdate(reqLogger, instance)
	if err != nil {
		return err
	}
	return nil
}
