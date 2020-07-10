package customdomain

import (
	"context"
	"fmt"
	"testing"
	"time"

	customdomainv1alpha1 "github.com/openshift/custom-domains-operator/pkg/apis/customdomain/v1alpha1"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	cloudingressv1alpha1 "github.com/openshift/cloud-ingress-operator/pkg/apis/cloudingress/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

// TestCustomDomainController runs ReconcileCustomDomain.Reconcile() against a
// fake client that tracks a CustomDomain object.
func TestCustomDomainController(t *testing.T) {
	// Set the logger to development mode for verbose logs.
	logf.SetLogger(logf.ZapLogger(true))

	var (
		name          = "cluster"
		namespace     = "default"
		domain        = "apps.foo.com"
		basedomain    = "apps.openshiftapps.com"
		newSecretName = "my-tls"
		newSecretData = "DEADBEEF"
		oldSecretName = "old-tls"
		oldSecretData = "0BADBEEF"
		oldDomain     = "old.domain.com"
	)

	// A CustomDomain resource with metadata and spec.
	customdomain := &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain:    domain,
			TLSSecret: newSecretName,
		},
	}

	// old secret of type kubernetes.io/tls
	oldTlsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      oldSecretName,
			Namespace: "openshift-ingress",
		},
		Data: make(map[string][]byte),
		Type: corev1.SecretTypeTLS,
	}
	oldTlsSecret.Data[corev1.TLSPrivateKeyKey] = []byte(oldSecretData)
	oldTlsSecret.Data[corev1.TLSCertKey] = []byte(oldSecretData)

	// new secret of type kubernetes.io/tls
	newTlsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      newSecretName,
			Namespace: "openshift-ingress",
		},
		Data: make(map[string][]byte),
		Type: corev1.SecretTypeTLS,
	}
	newTlsSecret.Data[corev1.TLSPrivateKeyKey] = []byte(newSecretData)
	newTlsSecret.Data[corev1.TLSCertKey] = []byte(newSecretData)

	// A Secret of type kubernetes.io/tls
	mockRouterCerts := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "router-certs",
			Namespace: "openshift-config-managed",
		},
		Data: make(map[string][]byte),
	}
	mockRouterCerts.Data[""] = []byte("")

	// dns.operator.openshift.io/default
	dnsOperator := &operatorv1.DNS{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "",
		},
		Spec: operatorv1.DNSSpec{
			Servers: []operatorv1.Server{},
		},
	}

	// dns.config.openshift.io/cluster
	dnsConfig := &configv1.DNS{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster",
			Namespace: "",
		},
		Spec: configv1.DNSSpec{
			BaseDomain: oldDomain,
			PublicZone: &configv1.DNSZone{
				ID: "12345",
			},
			PrivateZone: &configv1.DNSZone{
				ID: "12345",
			},
		},
	}

	// ingresscontrollers.operator.openshift.io/default
	defaultIngress := &operatorv1.IngressController{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "openshift-ingress-operator",
		},
		Spec: operatorv1.IngressControllerSpec{
			Domain: oldDomain,
			DefaultCertificate: &corev1.LocalObjectReference{
				Name: oldSecretName,
			},
		},
	}

	// ingress.config.openshift.io/cluster
	ingressConfig := &configv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster",
			Namespace: "",
		},
		Spec: configv1.IngressSpec{
			Domain: oldDomain,
		},
	}

	// apiserver.config.openshift.io/cluster
	apiserverConfig := &configv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster",
			Namespace: "",
		},
		Spec: configv1.APIServerSpec{
			ServingCerts: configv1.APIServerServingCerts{
				NamedCertificates: []configv1.APIServerNamedServingCert{
					configv1.APIServerNamedServingCert{
						Names: []string{"api." + oldDomain},
					},
				},
			},
		},
	}

	// publishingstrategy
	publishingStrategy := &cloudingressv1alpha1.PublishingStrategy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "publishingstrategy",
			Namespace: "openshift-cloud-ingress-operator",
		},
		Spec: cloudingressv1alpha1.PublishingStrategySpec{
			ApplicationIngress: []cloudingressv1alpha1.ApplicationIngress{
				cloudingressv1alpha1.ApplicationIngress{
					Listening: "external",
					Default:   true,
					DNSName:   oldDomain,
					Certificate: corev1.SecretReference{
						Name:      oldSecretName,
						Namespace: "openshift-ingress",
					},
				},
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{
		customdomain,
		oldTlsSecret,
		newTlsSecret,
		mockRouterCerts,
		dnsOperator,
		dnsConfig,
		defaultIngress,
		ingressConfig,
		apiserverConfig,
		publishingStrategy,
	}

	for n, ns := range systemRoutes {
		// a mock route
		sysRoute := &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n,
				Namespace: ns,
			},
			Spec: routev1.RouteSpec{
				Host: n + "." + basedomain,
			},
		}
		objs = append(objs, sysRoute)
	}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme

	// Add route Openshift scheme
	if err := routev1.AddToScheme(s); err != nil {
		t.Fatalf("Unable to add route scheme: (%v)", err)
	}

	// Add Openshift operator v1 scheme
	if err := operatorv1.AddToScheme(s); err != nil {
		t.Fatalf("Unable to add operatorv1 scheme: (%v)", err)
	}

	// Add Openshift config v1 scheme
	if err := configv1.AddToScheme(s); err != nil {
		t.Fatalf("Unable to add configv1 scheme: (%v)", err)
	}

	// Add Openshift cloudingressv1alpha
	if err := cloudingressv1alpha1.SchemeBuilder.AddToScheme(s); err != nil {
		t.Fatalf("Unable to add cloudingressv1alpha1 scheme: (%v)", err)
	}

	s.AddKnownTypes(customdomainv1alpha1.SchemeGroupVersion, customdomain)

	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(objs...)

	log.Info("Creating ReconcileCustomDomain")
	// Create a ReconcileCustomDomain object with the scheme and fake client.
	r := &ReconcileCustomDomain{client: cl, scheme: s}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
	log.Info("Calling Reconcile()")
	res, err := r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Check the result of reconciliation to make sure it has the desired state.
	if res.Requeue {
		t.Error("reconcile requeue which is not expected")
	}

	// check actual router-certs secret
	actualRouterCerts := &corev1.Secret{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name:      "router-certs",
		Namespace: "openshift-config-managed",
	}, actualRouterCerts)
	if _, ok := actualRouterCerts.Data[domain]; !ok {
		t.Errorf(fmt.Sprintf("Missing domain key from router-certs: (%v)", domain))
	}

	// check actual dns.operator/default
	actualDnsOperator := &operatorv1.DNS{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name: "default",
	}, actualDnsOperator)
	if actualDnsOperator.Spec.Servers[0].Name != name {
		t.Errorf(fmt.Sprintf("CRD dns.operator/default name mismatch: (%v)", actualDnsOperator.Spec.Servers[0].Name))
	}
	if actualDnsOperator.Spec.Servers[0].Zones[0] != domain {
		t.Errorf(fmt.Sprintf("CRD dns.operator/default zones mismatch: (%v)", actualDnsOperator.Spec.Servers[0].Zones[0]))
	}

	// check actual ingresscontrollers.operator.openshift.io/default
	actualDefaultIngress := &operatorv1.IngressController{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name:      "default",
		Namespace: "openshift-ingress-operator",
	}, actualDefaultIngress)
	if actualDefaultIngress.Spec.Domain != domain {
		t.Errorf(fmt.Sprintf("CRD ingresscontrollers.operator.openshift.io/default domain mismatch: (%v)", actualDefaultIngress.Spec.Domain))
	}

	// check actual ingresses.config.openshift.io/cluster
	actualIngressConfig := &configv1.Ingress{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name:      "cluster",
		Namespace: "",
	}, actualIngressConfig)
	if actualIngressConfig.Spec.Domain != domain {
		t.Errorf(fmt.Sprintf("CRD ingresses.config.openshift.io/cluster domain mismatch: (%v)", actualIngressConfig.Spec.Domain))
	}

	// check annotations
	actualCustomDomain := &customdomainv1alpha1.CustomDomain{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, actualCustomDomain)
	if actualCustomDomain.ObjectMeta.Annotations == nil || actualCustomDomain.ObjectMeta.Annotations["original-domain"] != oldDomain {
		t.Errorf(fmt.Sprintf("Problem with 'original-domain' annotation: (%+v)", actualCustomDomain.ObjectMeta.Annotations))
	}
	if actualCustomDomain.ObjectMeta.Annotations == nil || actualCustomDomain.ObjectMeta.Annotations["original-certificate"] != oldSecretName {
		t.Errorf(fmt.Sprintf("Problem with 'original-certificate' annotation: (%+v)", actualCustomDomain.ObjectMeta.Annotations))
	}

	// check for ready status
	if actualCustomDomain.Status.State != customdomainv1alpha1.CustomDomainStateReady {
		t.Errorf(fmt.Sprintf("Status.State does not equal (%s)", string(customdomainv1alpha1.CustomDomainStateReady)))
	}

	// Check resulting modified 'system' routes
	for n, ns := range systemRoutes {
		actualRoute := &routev1.Route{}
		expectedRoute := &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n,
				Namespace: ns,
			},
			Spec: routev1.RouteSpec{
				Host: n + "." + domain,
			},
		}
		err = r.client.Get(context.TODO(), types.NamespacedName{
			Name:      expectedRoute.Name,
			Namespace: expectedRoute.Namespace,
		}, actualRoute)
		if err != nil {
			t.Errorf("get customdomain: (%v)", err)
			continue
		}
		if actualRoute.Name != expectedRoute.Name {
			t.Error(fmt.Sprintf("actual route name (%v) does not match expected route name (%v)", actualRoute.Name, expectedRoute.Name))
			continue
		}
		if actualRoute.Namespace != expectedRoute.Namespace {
			t.Error(fmt.Sprintf("actual route namespace (%v) does not match expected route namespace (%v)", actualRoute.Namespace, expectedRoute.Namespace))
			continue
		}
		if actualRoute.Spec.Host != expectedRoute.Spec.Host {
			t.Error(fmt.Sprintf("actual route host (%v) does not match expected route host (%v)", actualRoute.Spec.Host, expectedRoute.Spec.Host))
			continue
		}
	}

	// Reconcile again so Reconcile() checks routes and updates the CustomDomain
	// resources' Status.
	res, err = r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	if res != (reconcile.Result{}) {
		t.Error("reconcile did not return an empty Result")
	}

	// Get the updated CustomDomain object.
	customdomain = &customdomainv1alpha1.CustomDomain{}
	err = r.client.Get(context.TODO(), req.NamespacedName, customdomain)
	if err != nil {
		t.Errorf("get customdomain: (%v)", err)
	}
	// check that status is ready
	if customdomain.Status.State != customdomainv1alpha1.CustomDomainStateReady {
		t.Errorf("Status is not Ready")
	}

	// simulate deletion
	now := metav1.NewTime(time.Now())
	customdomain.SetDeletionTimestamp(&now)
	err = r.client.Update(context.TODO(), customdomain)
	if err != nil {
		t.Fatalf("customdomain update: (%v)", err)
	}
	res, err = r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	err = r.client.Get(context.TODO(), req.NamespacedName, customdomain)
	if err != nil {
		t.Errorf("get customdomain: (%v)", err)
	}
	customdomain.SetFinalizers(remove(customdomain.GetFinalizers(), customDomainFinalizer))
	err = r.client.Update(context.TODO(), customdomain)
	if err != nil {
		t.Fatalf("customdomain update: (%v)", err)
	}
	res, err = r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// check that deletion of the customdomain and reconcile path
	err = r.client.Delete(context.TODO(), customdomain)
	if err != nil {
		t.Errorf("delete customdomain: (%v)", err)
	}
	res, err = r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	if res != (reconcile.Result{}) {
		t.Error("reconcile did not return an empty Result")
	}
	res, err = r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	if res != (reconcile.Result{}) {
		t.Error("reconcile did not return an empty Result")
	}
}
