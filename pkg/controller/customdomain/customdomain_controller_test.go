package customdomain

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	customdomainv1alpha1 "github.com/dustman9000/custom-domain-operator/pkg/apis/customdomain/v1alpha1"

	routev1 "github.com/openshift/api/route/v1"
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
		name       = "acme"
		namespace  = "default"
		domain     = "apps.foo.com"
		basedomain = "apps.openshiftapps.com"
		secretName = "tls"
		secretData = "DEADBEEF"
	)

	// A CustomDomain resource with metadata and spec.
	customdomain := &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain: domain,
			TLSSecret: corev1.ObjectReference{
				Name:      secretName,
				Namespace: namespace,
			},
		},
	}

	// A Secret of type kubernetes.io/tls
	tlsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: make(map[string][]byte),
		Type: corev1.SecretTypeTLS,
	}
	tlsSecret.Data[corev1.TLSPrivateKeyKey] = []byte(secretData)
	tlsSecret.Data[corev1.TLSCertKey] = []byte(secretData)

	// Objects to track in the fake client.
	objs := []runtime.Object{
		customdomain,
		tlsSecret,
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

	// Create the expected routes in namespace and collect their names to check
	// later.
	routeLabels := labelsForRoute(name)
	tlsConfig := &routev1.TLSConfig{
		Key: secretData,
		Certificate: secretData,
	}
	for n, ns := range systemRoutes {
		actualRoute := &routev1.Route{}
		expectedRoute := &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n + "-" + name,
				Namespace: ns,
				Labels:    routeLabels,
			},
			Spec: routev1.RouteSpec{
				Host: n + "." + domain,
				TLS:  tlsConfig,
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
		if !reflect.DeepEqual(actualRoute.Labels, expectedRoute.Labels) {
			t.Error("actual route labels do not match expected route labels")
			continue
		}
		if actualRoute.Spec.Host != expectedRoute.Spec.Host {
			t.Error(fmt.Sprintf("actual route host (%v) does not match expected route host (%v)", actualRoute.Spec.Host, expectedRoute.Spec.Host))
			continue
		}
		if !reflect.DeepEqual(actualRoute.Spec.TLS, expectedRoute.Spec.TLS) {
			t.Error("actual route tls config does not match expected route tls config")
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
}
