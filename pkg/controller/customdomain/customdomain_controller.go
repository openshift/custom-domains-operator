package customdomain

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	cloudingressv1alpha1 "github.com/openshift/cloud-ingress-operator/pkg/apis/cloudingress/v1alpha1"
	customdomainv1alpha1 "github.com/openshift/custom-domains-operator/pkg/apis/customdomain/v1alpha1"
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
	if err := modifyClusterDomain(r, reqLogger, instance, false); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// modifyClusterDomain modifies the cluster domain
func modifyClusterDomain(r *ReconcileCustomDomain, reqLogger logr.Logger, instance *customdomainv1alpha1.CustomDomain, finalize bool) error {
	var (
		domain = ""
		cert   = ""
	)

	// if we are restoring the original domain, get original state from annotations
	if finalize {
		domain = instance.ObjectMeta.Annotations["original-domain"]
		cert = instance.ObjectMeta.Annotations["original-certificate"]
	} else {
		domain = instance.Spec.Domain
		cert = instance.Spec.TLSSecret
	}

	if !finalize {
		if instance.Status.State != customdomainv1alpha1.CustomDomainStateReady {
			// Update the status on CustomDomain
			SetCustomDomainStatus(
				reqLogger,
				instance,
				fmt.Sprintf("Creating Custom Domain (%s)", domain),
				customdomainv1alpha1.CustomDomainConditionCreating,
				customdomainv1alpha1.CustomDomainStateNotReady)
			r.statusUpdate(reqLogger, instance)
		}
		if instance.ObjectMeta.Annotations == nil {
			instance.ObjectMeta.Annotations = make(map[string]string)
		}
	}

	// look up tls secret (must exist in the openshift-ingress namespace)
	tlsSecret := &corev1.Secret{}
	err := r.client.Get(context.TODO(), types.NamespacedName{
		Namespace: "openshift-ingress",
		Name:      cert,
	}, tlsSecret)
	if err != nil {
		// secret needed to continue
		reqLogger.Info(fmt.Sprintf("Error getting secret (%v)!", cert))
		// Update the status on CustomDomain
		SetCustomDomainStatus(
			reqLogger,
			instance,
			fmt.Sprintf("TLS Secret (%s) Not Found", cert),
			customdomainv1alpha1.CustomDomainConditionSecretNotFound,
			customdomainv1alpha1.CustomDomainStateNotReady)
		r.statusUpdate(reqLogger, instance)
		return err
	}

	// modify router-certs for auth operator
	// TODO(drow): Determine is this is still needed
	routerCert := &corev1.Secret{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Namespace: "openshift-config-managed",
		Name:      "router-certs",
	}, routerCert)
	if err != nil {
		// Error reading the object - requeue the request.
		reqLogger.Error(err, "Error getting router-certs")
		SetCustomDomainStatus(
			reqLogger,
			instance,
			"Error getting router-certs",
			customdomainv1alpha1.CustomDomainConditionRouterCertsError,
			customdomainv1alpha1.CustomDomainStateNotReady)
		r.statusUpdate(reqLogger, instance)
		return err
	}
	if _, ok := routerCert.Data[domain]; !ok {
		certData := string(tlsSecret.Data[corev1.TLSCertKey])
		keyData := string(tlsSecret.Data[corev1.TLSPrivateKeyKey])
		routerCert.Data[domain] = []byte(certData + keyData)
		err = r.client.Update(context.TODO(), routerCert)
		if err != nil {
			log.Error(err, "Error updating router-certs")
			SetCustomDomainStatus(
				reqLogger,
				instance,
				"Error updating router-certs",
				customdomainv1alpha1.CustomDomainConditionRouterCertsError,
				customdomainv1alpha1.CustomDomainStateNotReady)
			r.statusUpdate(reqLogger, instance)
		}
	}

	// modify dns.operator.openshift.io/default on creation
	// TODO(drow): restore original dns.operator.openshift.io/default
	dnsOperator := &operatorv1.DNS{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name: "default",
	}, dnsOperator)
	if err != nil {
		reqLogger.Error(err, "Error getting dns.operator.openshift.io/default")
		// Error reading the object - requeue the request.
		return err
	}
	if !finalize {
		dnsServer := &operatorv1.Server{
			Name:  instance.Name,
			Zones: []string{domain},
			ForwardPlugin: operatorv1.ForwardPlugin{
				Upstreams: []string{"8.8.8.8"},
			},
		}
		// serialize and store original dns.operator/default
		if _, ok := instance.ObjectMeta.Annotations["original-dns-operator"]; !ok {
			encSpec, _ := json.Marshal(dnsOperator.Spec)
			instance.ObjectMeta.Annotations["original-dns-operator"] = string(encSpec)
		}
		if dnsOperator.Spec.Servers != nil && len(dnsOperator.Spec.Servers) > 0 {
			dnsOperator.Spec.Servers[0] = *dnsServer
		} else {
			dnsOperator.Spec.Servers = []operatorv1.Server{*dnsServer}
		}
	} else {
		// deserialize original dns.operator/default
		if _, ok := instance.ObjectMeta.Annotations["original-dns-operator"]; ok {
			decSpec := operatorv1.DNSSpec{}
			json.Unmarshal([]byte(instance.ObjectMeta.Annotations["original-dns-operator"]), &decSpec)
			dnsOperator.Spec = decSpec
		}
	}
	err = r.client.Update(context.TODO(), dnsOperator)
	if err != nil {
		log.Error(err, "Error restoring dns.operator.openshift.io/default")
	}

	// modify dnses.config.openshift.io/cluster
	dnsConfig := &configv1.DNS{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name: "cluster",
	}, dnsConfig)
	if err != nil {
		reqLogger.Error(err, "Error getting dnses.config.openshift.io/cluster")
		// Error reading the object - requeue the request.
		return err
	}
	if !finalize {
		// serialize and store original dns.config/cluster
		if _, ok := instance.ObjectMeta.Annotations["original-dns-config"]; !ok {
			encSpec, _ := json.Marshal(dnsConfig.Spec)
			instance.ObjectMeta.Annotations["original-dns-config"] = string(encSpec)
			err = r.client.Update(context.TODO(), instance)
			if err != nil {
				reqLogger.Error(err, "Error updating instance with 'original-dns-config' annotation")
			}
		}
		// get top-level domain name
		baseDomain := domain[strings.IndexByte(domain, '.')+1 : len(domain)]
		dnsConfig.Spec.BaseDomain = baseDomain
		// we must set the private and public zones to nil to tell the ingress operator to not manage the DNS
		dnsConfig.Spec.PrivateZone = nil
		dnsConfig.Spec.PublicZone = nil
	} else {
		// deserialize original dns.config/cluster
		if _, ok := instance.ObjectMeta.Annotations["original-dns-config"]; ok {
			decSpec := configv1.DNSSpec{}
			json.Unmarshal([]byte(instance.ObjectMeta.Annotations["original-dns-config"]), &decSpec)
			dnsConfig.Spec = decSpec
		}
	}
	err = r.client.Update(context.TODO(), dnsConfig)
	if err != nil {
		log.Error(err, "Error updating dnses.config.openshift.io/cluster")
	}

	// modify ingresscontrollers.openshift.io/default
	defaultIngress := &operatorv1.IngressController{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Namespace: "openshift-ingress-operator",
		Name:      "default",
	}, defaultIngress)
	if err != nil {
		reqLogger.Error(err, "Error getting default ingresscontroller")
		// Error reading the object - requeue the request.
		return err
	}
	// Only update annotations when creating new CustomDomain
	if !finalize {
		if _, ok := instance.ObjectMeta.Annotations["original-domain"]; !ok {
			instance.ObjectMeta.Annotations["original-domain"] = defaultIngress.Spec.Domain
		}
		if _, ok := instance.ObjectMeta.Annotations["original-default-ingresscontroller"]; !ok {
			encSpec, _ := json.Marshal(defaultIngress.Spec)
			// update status with old domain and tls secret
			instance.ObjectMeta.Annotations["original-default-ingresscontroller"] = string(encSpec)
			err = r.client.Update(context.TODO(), instance)
			if err != nil {
				reqLogger.Error(err, "Error updating instance with original annotations")
			}
		}
		defaultIngress.Spec.Domain = domain
		if defaultIngress.Spec.DefaultCertificate != nil {
			if _, ok := instance.ObjectMeta.Annotations["original-certificate"]; !ok {
				instance.ObjectMeta.Annotations["original-certificate"] = defaultIngress.Spec.DefaultCertificate.Name
			}
			defaultIngress.Spec.DefaultCertificate.Name = cert
		} else {
			defaultIngress.Spec.DefaultCertificate = &corev1.LocalObjectReference{}
			defaultIngress.Spec.DefaultCertificate.Name = cert
		}
	} else {
		// deserialize original dns.operator/default
		if _, ok := instance.ObjectMeta.Annotations["original-default-ingresscontroller"]; ok {
			decSpec := operatorv1.IngressControllerSpec{}
			json.Unmarshal([]byte(instance.ObjectMeta.Annotations["original-default-ingresscontroller"]), &decSpec)
			defaultIngress.Spec = decSpec
		}
	}
	err = r.client.Update(context.TODO(), defaultIngress)
	if err != nil {
		reqLogger.Error(err, "Error updating default ingress controller")
		return err
	}

	// ingresses.config.openshift.io/cluster
	ingressConfig := &configv1.Ingress{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name: "cluster",
	}, ingressConfig)
	if err != nil {
		reqLogger.Error(err, "Error getting ingresses.config.openshift.io/cluster")
		// Error reading the object - requeue the request.
		return err
	}
	ingressConfig.Spec.Domain = domain
	err = r.client.Update(context.TODO(), ingressConfig)
	if err != nil {
		reqLogger.Error(err, "Error updating ingresses.config.openshift.io/cluster")
		return err
	}

	// modify publishingstrategies.cloudingress.managed.openshift.io/publishingstrategy
	publishingStrategy := &cloudingressv1alpha1.PublishingStrategy{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Namespace: "openshift-cloud-ingress-operator",
		Name:      "publishingstrategy",
	}, publishingStrategy)
	if err != nil {
		reqLogger.Error(err, "Error getting default publishingstrategy")
	} else {
		if !finalize {
			if _, ok := instance.ObjectMeta.Annotations["original-publishingstrategy"]; !ok {
				encSpec, _ := json.Marshal(publishingStrategy.Spec)
				// update status with old domain and tls secret
				instance.ObjectMeta.Annotations["original-publishingstrategy"] = string(encSpec)
				err = r.client.Update(context.TODO(), instance)
				if err != nil {
					reqLogger.Error(err, "Error updating instance with original annotations")
				}
			}
			appIngress := &cloudingressv1alpha1.ApplicationIngress{
				Listening: "external",
				Default:   true,
				DNSName:   domain,
				Certificate: corev1.SecretReference{
					Name:      cert,
					Namespace: "openshift-ingress",
				},
			}
			publishingStrategy.Spec.ApplicationIngress[0] = *appIngress
		} else {
			// deserialize original publishingstrategy
			if _, ok := instance.ObjectMeta.Annotations["original-publishingstrategy"]; ok {
				decSpec := cloudingressv1alpha1.PublishingStrategySpec{}
				json.Unmarshal([]byte(instance.ObjectMeta.Annotations["original-publishingstrategy"]), &decSpec)
				publishingStrategy.Spec = decSpec
			}
		}
		err = r.client.Update(context.TODO(), publishingStrategy)
		if err != nil {
			reqLogger.Error(err, "Error updating publishingstrategy")
		}
	}

	// modify 'system' routes Host fields
	systemRoute := &routev1.Route{}
	for n, ns := range systemRoutes {
		err = r.client.Get(context.TODO(), types.NamespacedName{
			Name:      n,
			Namespace: ns,
		}, systemRoute)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Error getting route %v in namespace %v", n, ns))
			// Error reading the object - requeue the request.
			return err
		}
		if !finalize {
			// If we are not restoring the original domain, we need to maintain the original routes for access
			// look up the old tls secret (must exist in the openshift-ingress namespace)
			originalSecret := &corev1.Secret{}
			err := r.client.Get(context.TODO(), types.NamespacedName{
				Namespace: "openshift-ingress",
				Name:      instance.ObjectMeta.Annotations["original-certificate"],
			}, originalSecret)
			if err != nil {
				// secret needed to continue
				reqLogger.Info(fmt.Sprintf("Error getting secret (%s)!", instance.ObjectMeta.Annotations["original-certificate"]))
				return err
			}
			newRoute := duplicateRoute(systemRoute, originalSecret, instance.ObjectMeta.Annotations["original-domain"])
			// Check if the route exists
			existingRoute := &routev1.Route{}
			err = r.client.Get(context.TODO(), types.NamespacedName{
				Name:      newRoute.Name,
				Namespace: newRoute.Namespace,
			}, existingRoute)

			if err != nil {
				// Create route
				reqLogger.Info(fmt.Sprintf("Creating original Route %v with host %v", newRoute.Name, newRoute.Spec.Host))
				_, err = createRoute(reqLogger, context.TODO(), r.client, newRoute)
				if err != nil {
					log.Error(err, "Error creating route")
					return err
				}
			} else {
				if !reflect.DeepEqual(existingRoute.Spec.TLS, newRoute.Spec.TLS) ||
					existingRoute.Spec.Host != newRoute.Spec.Host {
					// Update existingRoute with TLS and domain fields from newRoute
					existingRoute.Spec.TLS = newRoute.Spec.TLS
					existingRoute.Spec.Host = newRoute.Spec.Host
					reqLogger.Info(fmt.Sprintf("Updating Route %v with host %v", newRoute.Name, newRoute.Spec.Host))
					err = r.client.Update(context.TODO(), existingRoute)
					if err != nil {
						log.Error(err, "Error updating route")
					}
				} else {
					reqLogger.Info(fmt.Sprintf("Route %v with host %v already exists", newRoute.Name, newRoute.Spec.Host))
				}
			}
		} else {
			// Delete the duplicated original routes if we are restoring the original domain
			routeList := &routev1.RouteList{}
			listOpts := []client.ListOption{
				client.MatchingLabels(labelsForResource(domain)),
			}
			ctx := context.TODO()
			err := r.client.List(ctx, routeList, listOpts...)
			for _, rt := range routeList.Items {
				err = r.client.Delete(ctx, &rt, client.GracePeriodSeconds(15))
				if err != nil {
					reqLogger.Error(err, fmt.Sprintf("Failed to call client.Delete() for %v", rt.Name))
				}
			}
		}

		// Modify the host portion of the existing routes
		// We must delete and recreate as the Host field is immutable
		if !strings.Contains(systemRoute.Spec.Host, domain) {
			systemRouteCopy := &routev1.Route{}
			systemRouteCopy.Spec = systemRoute.Spec
			systemRouteCopy.ObjectMeta = metav1.ObjectMeta{
				Name:      systemRoute.ObjectMeta.Name,
				Namespace: systemRoute.ObjectMeta.Namespace,
				Labels:    systemRoute.ObjectMeta.Labels,
			}
			hostPart := strings.Split(systemRoute.Spec.Host, ".")[0]
			systemRouteCopy.Spec.Host = hostPart + "." + domain

			err = r.client.Delete(context.TODO(), systemRoute)
			if err != nil {
				reqLogger.Error(err, "Error deleting route "+systemRoute.Name)
			}

			err = r.client.Create(context.TODO(), systemRouteCopy)
			if err != nil {
				reqLogger.Error(err, "Error creating route "+systemRouteCopy.Name)
			}
		}
	}

	if !finalize {
		// Update the status on CustomDomain
		SetCustomDomainStatus(
			reqLogger,
			instance,
			fmt.Sprintf("Custom Domain (%s) Is Ready", domain),
			customdomainv1alpha1.CustomDomainConditionReady,
			customdomainv1alpha1.CustomDomainStateReady)
		r.statusUpdate(reqLogger, instance)
	}
	return nil
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
func duplicateRoute(srcRoute *routev1.Route, tlsSecret *corev1.Secret, domain string) *routev1.Route {
	labels := labelsForResource(domain)
	for k, v := range srcRoute.ObjectMeta.Labels {
		labels[k] = v
	}
	destRoute := &routev1.Route{}
	destRoute.TypeMeta = metav1.TypeMeta{
		Kind:       "Route",
		APIVersion: "route.openshift.io/v1",
	}
	destRoute.ObjectMeta = metav1.ObjectMeta{
		Name:      srcRoute.ObjectMeta.Name + "-original",
		Namespace: srcRoute.ObjectMeta.Namespace,
		Labels:    labels,
	}
	tlsConfig := createTlsConfig(tlsSecret)
	destRoute.Spec = srcRoute.Spec
	hostPart := strings.Split(srcRoute.Spec.Host, ".")[0]
	destRoute.Spec.Host = hostPart + "." + domain
	destRoute.Spec.TLS = tlsConfig
	if srcRoute.Spec.TLS != nil {
		destRoute.Spec.TLS.Termination = srcRoute.Spec.TLS.Termination
		destRoute.Spec.TLS.InsecureEdgeTerminationPolicy = srcRoute.Spec.TLS.InsecureEdgeTerminationPolicy
	}
	return destRoute
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

// labelsForResource creates a simple set of labels for all resources
func labelsForResource(name string) map[string]string {
	return map[string]string{"custom-domains-operator-owner": name}
}
