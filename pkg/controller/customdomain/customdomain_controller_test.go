package customdomain

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	operatoringressv1 "github.com/openshift/api/operatoringress/v1"
	customdomainv1alpha1 "github.com/openshift/custom-domains-operator/pkg/apis/customdomain/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		clusterDomain             = "cluster1.x8s0.s1.openshiftapps.com"
		instanceName              = "test"
		instanceNameInvalidSecret = "invalid-secret"
		instanceNameValidSecret   = "valid-secret"
		instanceNamespace         = "my-project"
		instanceScope             = "Internal"
		invalidObjectNames        = [...]string{"-test", "t#st", "te.st", "tEst"}
		validScopeNames           = [...]string{"", "Internal", "External"}
		userNamespace             = "my-project"
		userDomain                = "apps.foo.com"
		userSecretName            = "my-secret"
		validSecretName           = "valid-secret"
		userSecretData            = "DEADBEEF"
		validSecretData           = "GROUNDBEEF"
	)

	// A CustomDomain resource with metadata and spec.
	customdomain := &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: userNamespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain: userDomain,
			Scope:  instanceScope,
			Certificate: corev1.SecretReference{
				Name:      userSecretName,
				Namespace: userNamespace,
			},
		},
	}

	// A CustomDomain with an invalid secret
	customdomainInvalidSecret := &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceNameInvalidSecret,
			Namespace: userNamespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain: userDomain,
			Scope:  "",
			Certificate: corev1.SecretReference{
				Name:      "invalid",
				Namespace: userNamespace,
			},
		},
	}

	// A CustomDomain with a valid secret
	customdomainValidSecret := &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceNameValidSecret,
			Namespace: userNamespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain: userDomain,
			Scope:  "",
			Certificate: corev1.SecretReference{
				Name:      validSecretName,
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

	// valid secret of type kubernetes.io/tls
	validSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      validSecretName,
			Namespace: userNamespace,
		},
		Data: make(map[string][]byte),
		Type: corev1.SecretTypeTLS,
	}
	validSecret.Data[corev1.TLSPrivateKeyKey] = []byte(validSecretData)
	validSecret.Data[corev1.TLSCertKey] = []byte(validSecretData)

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
		customdomainInvalidSecret,
		customdomainValidSecret,
		userSecret,
		validSecret,
	}

	// generate CustomDomains w/ restricted ingress names
	for _, n := range restrictedIngressNames {
		cd := &customdomainv1alpha1.CustomDomain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n,
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
		ing := &operatorv1.IngressController{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n,
				Namespace: ingressOperatorNamespace,
			},
			Spec: operatorv1.IngressControllerSpec{
				Domain: userDomain,
				DefaultCertificate: &corev1.LocalObjectReference{
					Name: userSecretName,
				},
			},
		}
		objs = append(objs, cd)
		objs = append(objs, ing)
	}

	// generate CustomDomains with valid secret

	cd := &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "validSecretCustomDomain",
			Namespace: userNamespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain: userDomain,
			Certificate: corev1.SecretReference{
				Name:      validSecretName,
				Namespace: userNamespace,
			},
		},
	}
	ing := &operatorv1.IngressController{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "validSecretIngress",
			Namespace: ingressOperatorNamespace,
		},
		Spec: operatorv1.IngressControllerSpec{
			Domain: userDomain,
			DefaultCertificate: &corev1.LocalObjectReference{
				Name: validSecretName,
			},
		},
	}
	objs = append(objs, cd)
	objs = append(objs, ing)

	// Customdomains w/ invalid object names
	for _, n := range invalidObjectNames {
		cd := &customdomainv1alpha1.CustomDomain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n,
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
		objs = append(objs, cd)
	}

	// Customdomains w/ valid scope names
	for _, n := range validScopeNames {
		cd := &customdomainv1alpha1.CustomDomain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n,
				Namespace: userNamespace,
			},
			Spec: customdomainv1alpha1.CustomDomainSpec{
				Domain: userDomain,
				Certificate: corev1.SecretReference{
					Name:      userSecretName,
					Namespace: userNamespace,
				},
				Scope: n,
			},
		}
		objs = append(objs, cd)
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

	// ========= TEST RECONCILE REQUEST =========
	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      instanceName,
			Namespace: instanceNamespace,
		},
	}

	// test reconcile w/ valid Secret
	res, err := r.Reconcile(req)
	if err == nil {
		t.Fatalf("reconcile, expected error w/ valid Secret")
	}

	if r.client.Create(context.TODO(), customdomainValidSecret) == nil {
		t.Fatalf("reconcile, error w/ customdomainValidSecret")
	}

	// test reconcile w/ missing dnsConfig
	res, err = r.Reconcile(req)
	if err == nil {
		t.Fatalf("reconcile, expected error w/ missing dnsConfig")
	}

	if r.client.Create(context.TODO(), dnsConfig) != nil {
		t.Fatalf("reconcile, error w/ dnsConfig")
	}

	// test reconcile w/ missing dnsRecord
	res, err = r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile, returned error w/ missing dnsRecord")
	}

	if r.client.Create(context.TODO(), dnsRecord) != nil {
		t.Fatalf("reconcile, error w/ dnsRecord")
	}

	res, err = r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Check the result of reconciliation to make sure it has the desired state.
	if res.Requeue {
		t.Error("reconcile requeue which is not expected")
	}

	// Check reconcile of customdomain with invalid secret
	reqInvalidSecret := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      instanceNameInvalidSecret,
			Namespace: instanceNamespace,
		},
	}
	res, err = r.Reconcile(reqInvalidSecret)
	if err == nil {
		t.Fatalf("Expected an error for %s CustomDomain", instanceNameInvalidSecret)
	}

	// Check reconcile of customdomain with valid secret
	reqValidSecret := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      instanceNameValidSecret,
			Namespace: instanceNamespace,
		},
	}

	res, err = r.Reconcile(reqValidSecret)
	if err != nil {
		t.Fatalf("Expected an error for %s CustomDomain", "validSecretCustomDomain")
	}

	// Test reconcile of customdomain with restricted ingress name
	for _, n := range restrictedIngressNames {
		// Check reconcile of customdomain with invalid ingress name
		reqInvalidName := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      n,
				Namespace: instanceNamespace,
			},
		}
		res, err = r.Reconcile(reqInvalidName)
		if err == nil {
			t.Fatalf("Expected an error for %s CustomDomain", n)
		}
	}

	// Test reconcile of customdomain with restricted object name
	for _, n := range invalidObjectNames {
		// Check reconcile of customdomain with invalid object name
		reqInvalidName := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      n,
				Namespace: instanceNamespace,
			},
		}
		res, err = r.Reconcile(reqInvalidName)
		if err == nil {
			t.Fatalf("Expected an error for %s CustomDomain", n)
		}
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
	if string(actualCustomIngress.Spec.EndpointPublishingStrategy.LoadBalancer.Scope) != instanceScope {
		t.Errorf(fmt.Sprintf("CRD ingresscontrollers.operator.openshift.io/default scope mismatch: (%v)", actualCustomIngress.Spec.EndpointPublishingStrategy.LoadBalancer.Scope))
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

	// Check scope immutability
	externalScopePatchData := []byte(`{"spec":{"scope":"External"}}`)
	err = r.client.Patch(context.TODO(), &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name: instanceName,
			Namespace: instanceNamespace,
		},
	}, client.ConstantPatch(types.StrategicMergePatchType, externalScopePatchData))
	if err != nil {
		t.Error("Unable to patch customdomain scope")
	}

	res, err = r.Reconcile(req)
	if err == nil {
		t.Error("Expected error when modifying Spec.Scope")
	}

	// Reset scope after testing
	internalScopePatchData := []byte(`{"spec":{"scope":"Internal"}}`)
	err = r.client.Patch(context.TODO(), &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name: instanceName,
			Namespace: instanceNamespace,
		},
	}, client.ConstantPatch(types.StrategicMergePatchType, internalScopePatchData))

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

	// ========= DELETION =========
	// deletion with restricted ingress names
	now := metav1.NewTime(time.Now())
	for _, n := range restrictedIngressNames {
		customdomain.Name = n
		req.NamespacedName.Name = n
		customdomain.SetDeletionTimestamp(&now)
		err = r.client.Update(context.TODO(), customdomain)
		if err != nil {
			t.Fatalf("update failed: (%v)", err)
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
	}

	// deletion with restricted object names
	for _, n := range invalidObjectNames {
		customdomain.Name = n
		req.NamespacedName.Name = n
		customdomain.SetDeletionTimestamp(&now)
		err = r.client.Update(context.TODO(), customdomain)
		if err != nil {
			t.Fatalf("update failed: (%v)", err)
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
	}

	// simulate deletion of customdomain
	customdomain.Name = instanceName
	req.NamespacedName.Name = instanceName
	err = r.client.Update(context.TODO(), customdomain)
	if err != nil {
		t.Fatalf("update failed: (%v)", err)
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

	// check that ingresscontroller was deleted
	err = r.client.Get(context.TODO(), types.NamespacedName{
		Name:      instanceName,
		Namespace: ingressOperatorNamespace,
	}, actualCustomIngress)
	if err == nil {
		t.Fatalf("ingresscontroller %s was not deleted!", instanceName)
	}
}
