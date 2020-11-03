package customdomain

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	customdomainv1alpha1 "github.com/openshift/custom-domains-operator/pkg/apis/customdomain/v1alpha1"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	operatoringressv1 "github.com/openshift/api/operatoringress/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TestCustomDomainController runs ReconcileCustomDomain.Reconcile() against a
// fake client that tracks a CustomDomain object.
func TestCustomDomainController(t *testing.T) {
	// Set the logger to development mode for verbose logs.
	logf.SetLogger(zap.New(zap.UseDevMode(true)))

	var (
		clusterDomain     = "cluster1.x8s0.s1.openshiftapps.com"
		instanceName      = "test"
		instanceNamespace = "my-project"
		userNamespace     = "my-project"
		userDomain        = "apps.foo.com"
		userSecretName    = "my-secret"
		userSecretData    = "DEADBEEF"
	)

	// A CustomDomain resource with metadata and spec.
	customdomain := &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: userNamespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain: userDomain,
			Certificate: corev1.SecretReference{
				Name:      userSecretName,
				Namespace: userNamespace,
			},
		},
	}

	// new secret of type kubernetes.io/tls
	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      userSecretName,
			Namespace: userNamespace,
		},
		Data: make(map[string][]byte),
		Type: corev1.SecretTypeTLS,
	}
	userSecret.Data[corev1.TLSPrivateKeyKey] = []byte(userSecretData)
	userSecret.Data[corev1.TLSCertKey] = []byte(userSecretData)

	// dns.config.openshift.io/cluster
	dnsConfig := &configv1.DNS{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster",
			Namespace: "",
		},
		Spec: configv1.DNSSpec{
			BaseDomain: clusterDomain,
			PublicZone: &configv1.DNSZone{
				ID: "12345",
			},
			PrivateZone: &configv1.DNSZone{
				ID: "12345",
			},
		},
	}

	// dns.config.openshift.io/cluster
	dnsRecord := &operatoringressv1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName + "-wildcard",
			Namespace: ingressOperatorNamespace,
		},
		Spec: operatoringressv1.DNSRecordSpec{
			DNSName: "*." + instanceName + "." + clusterDomain,
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{
		customdomain,
		userSecret,
		dnsConfig,
		dnsRecord,
	}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme

	// Add Openshift operator v1 scheme
	if err := operatorv1.AddToScheme(s); err != nil {
		t.Fatalf("Unable to add operatorv1 scheme: (%v)", err)
	}

	// Add Openshift operatoringress v1 scheme
	if err := operatoringressv1.AddToScheme(s); err != nil {
		t.Fatalf("Unable to add operatoringressv1 scheme: (%v)", err)
	}

	// Add Openshift config v1 scheme
	if err := configv1.AddToScheme(s); err != nil {
		t.Fatalf("Unable to add configv1 scheme: (%v)", err)
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
			Name:      instanceName,
			Namespace: instanceNamespace,
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

	// check copied secret
	actualIngressSecret := &corev1.Secret{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name:      instanceName,
		Namespace: ingressNamespace,
	}, actualIngressSecret)
	if err != nil {
		t.Fatalf("get secret: (%v)", err)
	}
	if !reflect.DeepEqual(actualIngressSecret.Data, userSecret.Data) {
		t.Errorf(fmt.Sprintf("secret mismatch: (%s)", actualIngressSecret.Name))
	}

	// check actual ingresscontrollers.operator.openshift.io/default
	actualCustomIngress := &operatorv1.IngressController{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name:      instanceName,
		Namespace: ingressOperatorNamespace,
	}, actualCustomIngress)
	if err != nil {
		t.Fatalf("get ingress: (%v)", err)
	}
	if actualCustomIngress.Spec.Domain != instanceName+"."+clusterDomain {
		t.Errorf(fmt.Sprintf("CRD ingresscontrollers.operator.openshift.io/default domain mismatch: (%v)", actualCustomIngress.Spec.Domain))
	}

	// check instance
	actualCustomDomain := &customdomainv1alpha1.CustomDomain{}
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name:      instanceName,
		Namespace: instanceNamespace,
	}, actualCustomDomain)
	if err != nil {
		t.Fatalf("get custom domain: (%v)", err)
	}
	// check for ready status
	if actualCustomDomain.Status.State != customdomainv1alpha1.CustomDomainStateReady {
		t.Errorf(fmt.Sprintf("Status.State does not equal (%s)", string(customdomainv1alpha1.CustomDomainStateReady)))
	}

	// Reconcile again so Reconcile() and check result
	res, err = r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	if res != (reconcile.Result{}) {
		t.Error("reconcile did not return an empty Result")
	}

	// get instance
	err = r.client.Get(context.TODO(), req.NamespacedName, customdomain)
	if err != nil {
		t.Errorf("get customdomain: (%v)", err)
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

	log.Info("Deleting customdomain instance")
	// check the deletion of the customdomain and reconcile path
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

	// check copied secret
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name:      instanceName,
		Namespace: ingressNamespace,
	}, actualIngressSecret)
	if err == nil {
		t.Fatalf("secret %s was not deleted!", instanceName)
	}

	// check actual ingresscontrollers.operator.openshift.io/default
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name:      instanceName,
		Namespace: ingressOperatorNamespace,
	}, actualCustomIngress)
	if err == nil {
		t.Fatalf("get ingress: (%v)", err)
	}
}
