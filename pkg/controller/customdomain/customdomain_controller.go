package customdomain

import (
	"context"
	"fmt"

	customdomainv1alpha1 "github.com/dustman9000/custom-domain-operator/pkg/apis/customdomain/v1alpha1"
	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

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

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner CustomDomain
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

	// look up secret
	tlsSecret := &corev1.Secret{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Namespace: instance.Spec.TLSSecret.Namespace,
		Name:      instance.Spec.TLSSecret.Name,
	}, tlsSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			// continue if secret does not exist yet
			reqLogger.Info(fmt.Sprintf("Secret (%v) does not exist!", instance.Spec.TLSSecret.Name))
		} else {
			return reconcile.Result{}, err
		}
	}

	systemRoute := &routev1.Route{}
	for n, ns := range systemRoutes {
		err = r.client.Get(context.TODO(), types.NamespacedName{
			Name:      n,
			Namespace: ns,
		}, systemRoute)
		if err != nil {
			reqLogger.Info(fmt.Sprintf("Error getting route %v in namespace %v", n, ns), "Error", err.Error())
			// Error reading the object - requeue the request.
			return reconcile.Result{}, err
		}
		newRoute := duplicateRoute(systemRoute, tlsSecret, instance)
		// Check if route exists
		existingRoute := &routev1.Route{}
		err := r.client.Get(context.TODO(), types.NamespacedName{
			Name:      newRoute.Name,
			Namespace: newRoute.Namespace,
		}, existingRoute)

		if err != nil {
			// Create or Update route
			reqLogger.Info(fmt.Sprintf("Creating duplicate system Route %v with host %v", systemRoute.Name, systemRoute.Spec.Host))
			_, err = createRoute(reqLogger, context.TODO(), r.client, newRoute)
			if err != nil {
				log.Info("Error creating route", "Error", err.Error())
				return reconcile.Result{}, err
			}
		} else {
			reqLogger.Info(fmt.Sprintf("Dupicate system Route %v with host %v already exists", newRoute.Name, newRoute.Spec.Host))
		}
	}
	return reconcile.Result{}, nil
}

// finalizeCustomDomain cleans up once a CustomDomain CR is deleted
func (r *ReconcileCustomDomain) finalizeCustomDomain(reqLogger logr.Logger, m *customdomainv1alpha1.CustomDomain) error {
	// Delete all routes created by this operator
	routeList := &routev1.RouteList{}
	listOpts := []client.ListOption{
		client.MatchingLabels(labelsForRoute(m.ObjectMeta.Name)),
	}
	ctx := context.TODO()
	err := r.client.List(ctx, routeList, listOpts...)
	for _, rt := range routeList.Items {
		err = r.client.Delete(ctx, &rt, client.GracePeriodSeconds(5))
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Failed to call client.Delete() for %v", rt.ObjectMeta.Name))
			return err
		}
	}
	if err != nil {
		reqLogger.Error(err, "Failed to call client.List()")
		return err
	}
	reqLogger.Info("Successfully finalized customdomain")
	return nil
}

// addFinalizer is a function that adds a finalizer for the CustomDomain CR
func (r *ReconcileCustomDomain) addFinalizer(reqLogger logr.Logger, m *customdomainv1alpha1.CustomDomain) error {
	reqLogger.Info("Adding Finalizer for the CustomDomain")
	m.SetFinalizers(append(m.GetFinalizers(), customDomainFinalizer))

	// Update CR
	err := r.client.Update(context.TODO(), m)
	if err != nil {
		reqLogger.Error(err, "Failed to update CustomDomain with finalizer")
		return err
	}
	return nil
}

// contains is a helper function for finalizer
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

// createRoute is a function which creates the route
func createRoute(reqLogger logr.Logger, ctx context.Context, client client.Client, r *routev1.Route) (*routev1.Route, error) {
	if err := client.Create(ctx, r); err != nil {
		if err != nil {
			if !errors.IsAlreadyExists(err) {
				return nil, err
			}
			reqLogger.Info(fmt.Sprintf("Route object already exists (%v, %v)", r.Name, r.Namespace))
			return r, nil
		}
	}
	reqLogger.Info(fmt.Sprintf("Route object created (%v, %v)", r.Name, r.Namespace))
	return r, nil
}

// duplicateRoute creates a route to proxy to an existing system route
func duplicateRoute(systemRoute *routev1.Route, tlsSecret *corev1.Secret, instance *customdomainv1alpha1.CustomDomain) *routev1.Route {
	labels := labelsForRoute(instance.ObjectMeta.Name)
	for k, v := range systemRoute.ObjectMeta.Labels {
		labels[k] = v
	}
	newRoute := &routev1.Route{}
	newRoute.TypeMeta = metav1.TypeMeta{
		Kind:       "Route",
		APIVersion: "route.openshift.io/v1",
	}
	newRoute.ObjectMeta = metav1.ObjectMeta{
		Name:      systemRoute.ObjectMeta.Name + "-" + instance.ObjectMeta.Name,
		Namespace: systemRoute.ObjectMeta.Namespace,
		Labels:    labels,
	}
	tlsConfig := createTlsConfig(tlsSecret)
	newRoute.Spec = systemRoute.Spec
	newRoute.Spec.Host = systemRoute.ObjectMeta.Name + "." + instance.Spec.Domain
	newRoute.Spec.TLS = tlsConfig
	return newRoute
}

// createTlsConfig creates a new TLSConfig object
func createTlsConfig(tlsSecret *corev1.Secret) *routev1.TLSConfig {
	newTlsConfig := &routev1.TLSConfig{}
	// Secret must be of kubernetes.io/tls type that contains a tls.key and tls.crt
	if tlsSecret.Data != nil && tlsSecret.Type == corev1.SecretTypeTLS {
		keyData, ok := tlsSecret.Data[corev1.TLSPrivateKeyKey]
		if ok {
			newTlsConfig.Key = string(keyData)
		}
		crtData, ok := tlsSecret.Data[corev1.TLSCertKey]
		if ok {
			newTlsConfig.Certificate = string(crtData)
		}
	}
	return newTlsConfig
}

// labelsForRoute creates a simple set of labels for all routes.
func labelsForRoute(name string) map[string]string {
	return map[string]string{"custom-domain-operator-owner": name}
}
