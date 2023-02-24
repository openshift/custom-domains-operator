package managed

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	operatoringressv1 "github.com/openshift/api/operatoringress/v1"
	customdomainv1alpha1 "github.com/openshift/custom-domains-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		clusterDomain                    = "cluster1.x8s0.s1.openshiftapps.com"
		instanceName                     = "test"
		instanceNameInvalidSecret        = "invalid-secret"
		instanceNameValidSecret          = "valid-secret"
		instanceNameRouteSelectorNil     = "route-selector-nil"
		instanceNameRouteSelector        = "route-selector"
		instanceNameNamespaceSelectorNil = "namespace-selector-nil"
		instanceNameNamespaceSelector    = "namespace-selector"
		instanceNamespace                = "my-project"
		instanceScope                    = "Internal"
		invalidObjectNames               = [...]string{"-test", "t#st", "te.st", "tEst"}
		validScopeNames                  = [...]string{"", "Internal", "External"}
		userNamespace                    = "my-project"
		userDomain                       = "apps.foo.com"
		userSecretName                   = "my-secret"
		validSecretName                  = "valid-secret"
		userSecretData                   = "DEADBEEF"
		validSecretData                  = "GROUNDBEEF"
		routeLabels                      = map[string]string{"type": "public"}
		namespaceLabels                  = map[string]string{"kind": "core"}
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

	// A CustomDomain resource with routeSelector nil.
	customdomainRouteSelectorNil := &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceNameRouteSelectorNil,
			Namespace: userNamespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain: userDomain,
			Scope:  instanceScope,
			Certificate: corev1.SecretReference{
				Name:      userSecretName,
				Namespace: userNamespace,
			},
			RouteSelector:    nil,
			LoadBalancerType: "NLB",
		},
	}

	// A CustomDomain resource with routeSelector.
	customdomainRouteSelector := &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceNameRouteSelector,
			Namespace: userNamespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain: userDomain,
			Scope:  instanceScope,
			Certificate: corev1.SecretReference{
				Name:      userSecretName,
				Namespace: userNamespace,
			},
			RouteSelector: &metav1.LabelSelector{
				MatchLabels: routeLabels,
			},
		},
	}

	// A CustomDomain resource with namespaceSelector nil.
	customdomainNamespaceSelectorNil := &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceNameNamespaceSelectorNil,
			Namespace: userNamespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain: userDomain,
			Scope:  instanceScope,
			Certificate: corev1.SecretReference{
				Name:      userSecretName,
				Namespace: userNamespace,
			},
			NamespaceSelector: nil,
			LoadBalancerType:  "Classic",
		},
	}

	// A CustomDomain resource with namespaceSelector.
	customdomainNamespaceSelector := &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceNameNamespaceSelector,
			Namespace: userNamespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain: userDomain,
			Scope:  instanceScope,
			Certificate: corev1.SecretReference{
				Name:      userSecretName,
				Namespace: userNamespace,
			},
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: namespaceLabels,
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
	objs := []client.Object{
		customdomain,
		customdomainRouteSelector,
		customdomainRouteSelectorNil,
		customdomainNamespaceSelectorNil,
		customdomainNamespaceSelector,
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

	// generate CustomDomains with routeSelsctor

	cd := &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "routeSelectorCustomDomain",
			Namespace: userNamespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain: userDomain,
			Certificate: corev1.SecretReference{
				Name:      userSecretName,
				Namespace: userNamespace,
			},
			RouteSelector: &metav1.LabelSelector{
				MatchLabels: routeLabels,
			},
		},
	}
	ing := &operatorv1.IngressController{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "routeSelectorIngress",
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

	// generate CustomDomains with routeSelsctor nil

	cd = &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "routeSelectorCustomDomainNil",
			Namespace: userNamespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain: userDomain,
			Certificate: corev1.SecretReference{
				Name:      userSecretName,
				Namespace: userNamespace,
			},
			RouteSelector: nil,
		},
	}
	ing = &operatorv1.IngressController{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "routeSelectorNilIngress",
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

	// generate CustomDomains with namespaceSelsctor nil

	cd = &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "namespaceSelectorCustomDomainNil",
			Namespace: userNamespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain: userDomain,
			Scope:  instanceScope,
			Certificate: corev1.SecretReference{
				Name:      userSecretName,
				Namespace: userNamespace,
			},
			NamespaceSelector: nil,
		},
	}
	ing = &operatorv1.IngressController{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "namespaceSelectorNilIngress",
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

	// generate CustomDomains with namespaceSelsctor

	cd = &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "namespaceSelectorCustomDomain",
			Namespace: userNamespace,
		},
		Spec: customdomainv1alpha1.CustomDomainSpec{
			Domain: userDomain,
			Scope:  instanceScope,
			Certificate: corev1.SecretReference{
				Name:      userSecretName,
				Namespace: userNamespace,
			},
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: namespaceLabels,
			},
		},
	}
	ing = &operatorv1.IngressController{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "namespaceSelectorIngress",
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

	// generate CustomDomains with valid secret

	cd = &customdomainv1alpha1.CustomDomain{
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
	ing = &operatorv1.IngressController{
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

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster",
			Namespace: "",
		},
	}
	objs = append(objs, infra)

	// Create a fake client to mock API calls.
	cl := NewTestMock(t, objs...)
	if err := UpdatePlatformStatus(cl); err != nil {
		t.Fatalf("Unable to update cloudplatform type: {%v}", err)
	}

	t.Log("Creating CustomDomainReconciler")
	// Create a ReconcileCustomDomain object with the scheme and fake client.
	r := &CustomDomainReconciler{Client: cl, Scheme: cl.Scheme()}

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
	ctx := context.TODO()
	res, err := r.Reconcile(ctx, req)
	if err == nil {
		t.Fatalf("reconcile, expected error w/ valid Secret")
	}

	if r.Client.Create(context.TODO(), customdomainValidSecret) == nil {
		t.Fatalf("reconcile, error w/ customdomainValidSecret")
	}

	// test reconcile w/ missing dnsConfig
	res, err = r.Reconcile(ctx, req)
	if err == nil {
		t.Fatalf("reconcile, expected error w/ missing dnsConfig")
	}

	if r.Client.Create(context.TODO(), dnsConfig) != nil {
		t.Fatalf("reconcile, error w/ dnsConfig")
	}

	// test reconcile w/ missing dnsRecord
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile, returned error w/ missing dnsRecord")
	}

	if r.Client.Create(context.TODO(), dnsRecord) != nil {
		t.Fatalf("reconcile, error w/ dnsRecord")
	}

	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Check the result of reconciliation to make sure it has the desired state.
	if res.Requeue {
		t.Error("reconcile requeue which is not expected")
	}

	// Check reconcile of customdomain with routeSelector
	reqRouteSelector := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      instanceNameRouteSelector,
			Namespace: instanceNamespace,
		},
	}

	res, err = r.Reconcile(ctx, reqRouteSelector)
	if err != nil {
		t.Fatalf("Expected an error for %s CustomDomain", "routeSelectorCustomDomain")
	}

	// Check reconcile of customdomain with routeSelector nil
	reqRouteSelectorNil := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      instanceNameRouteSelectorNil,
			Namespace: instanceNamespace,
		},
	}

	res, err = r.Reconcile(ctx, reqRouteSelectorNil)
	if err != nil {
		t.Fatalf("Expected an error for %s CustomDomain", "routeSelectorCustomDomainNil")
	}

	// Check reconcile of customdomain with namespaceSelector
	reqNamespaceSelector := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      instanceNameNamespaceSelector,
			Namespace: instanceNamespace,
		},
	}

	res, err = r.Reconcile(ctx, reqNamespaceSelector)
	if err != nil {
		t.Fatalf("Expected an error for %s CustomDomain", "namespaceSelectorCustomDomain")
	}

	// Check reconcile of customdomain with namespaceSelector nil
	reqNamespaceSelectorNil := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      instanceNameNamespaceSelectorNil,
			Namespace: instanceNamespace,
		},
	}

	res, err = r.Reconcile(ctx, reqNamespaceSelectorNil)
	if err != nil {
		t.Fatalf("Expected an error for %s CustomDomain", "namespaceSelectorCustomDomainNil")
	}

	// Check reconcile of customdomain with invalid secret
	reqInvalidSecret := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      instanceNameInvalidSecret,
			Namespace: instanceNamespace,
		},
	}
	res, err = r.Reconcile(ctx, reqInvalidSecret)
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

	res, err = r.Reconcile(ctx, reqValidSecret)
	if err != nil {
		t.Fatalf("Expected an error for %s CustomDomain", "validSecretCustomDomain")
	}

	// Check that reconcile successfully added customdomain label to the userSecret
	validSecretReq := types.NamespacedName{
		Name:      validSecret.Name,
		Namespace: validSecret.Namespace,
	}
	err = r.Client.Get(context.TODO(), validSecretReq, validSecret)
	if err != nil {
		t.Fatalf("Error retrieving validSecret")
	}
	labelKey, found := validSecret.Labels[managedLabelName]
	if found != true {
		t.Error("reconcile, failed to add label to userSecret")
	} else if labelKey != instanceNameValidSecret {
		t.Error("reconcile, failed to label secret with correct customdomain name")
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
		res, err = r.Reconcile(ctx, reqInvalidName)
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
		res, err = r.Reconcile(ctx, reqInvalidName)
		if err == nil {
			t.Fatalf("Expected an error for %s CustomDomain", n)
		}
	}

	// check copied secret
	actualIngressSecret := &corev1.Secret{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{
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
	err = r.Client.Get(context.TODO(), types.NamespacedName{
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
	err = r.Client.Get(context.TODO(), types.NamespacedName{
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
	err = r.Client.Patch(context.TODO(), &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: instanceNamespace,
		},
	}, client.RawPatch(types.StrategicMergePatchType, externalScopePatchData))
	if err != nil {
		t.Error("Unable to patch customdomain scope")
	}

	res, err = r.Reconcile(ctx, req)
	if err == nil {
		t.Error("Expected error when modifying Spec.Scope")
	}

	// Reset scope after testing
	internalScopePatchData := []byte(`{"spec":{"scope":"Internal"}}`)
	err = r.Client.Patch(context.TODO(), &customdomainv1alpha1.CustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: instanceNamespace,
		},
	}, client.RawPatch(types.StrategicMergePatchType, internalScopePatchData))
	if err != nil {
		t.Error("Unable to patch customdomain scope")
	}
	// Reconcile again so Reconcile() and check result
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	if res != (reconcile.Result{}) {
		t.Error("reconcile did not return an empty Result")
	}

	// get instance
	err = r.Client.Get(context.TODO(), req.NamespacedName, customdomain)
	if err != nil {
		t.Errorf("get customdomain: (%v)", err)
	}

	// update certificate
	userSecret.Data[corev1.TLSCertKey] = []byte("newtestdata")
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	if res != (reconcile.Result{}) {
		t.Error("reconcile did not return empty Result")
	}
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Name:      actualIngressSecret.Name,
		Namespace: actualIngressSecret.Namespace,
	}, actualIngressSecret)
	if err != nil {
		t.Fatalf("failed to retrieve ingress secret %s", actualIngressSecret.Name)
	}
	if bytes.Equal(actualIngressSecret.Data[corev1.TLSCertKey], userSecret.Data[corev1.TLSCertKey]) {
		t.Fatalf("failed to update ingress secret")
	}

	// ========= DELETION =========
	// deletion with restricted ingress names
	now := metav1.NewTime(time.Now())
	for _, n := range restrictedIngressNames {
		customdomain.Name = n
		req.NamespacedName.Name = n
		err = r.Client.Get(context.TODO(), types.NamespacedName{
			Name:      customdomain.Name,
			Namespace: customdomain.Namespace,
		}, customdomain)
		if err != nil {
			t.Fatalf("get failed: (%v)", err)
		}
		log.Info("Deleting customdomain instance")
		customdomain.SetDeletionTimestamp(&now)
		err = r.Client.Update(context.TODO(), customdomain)
		if err != nil {
			t.Fatalf("update failed: (%v)", err)
		}
		res, err = r.Reconcile(ctx, req)
		if err != nil {
			t.Fatalf("reconcile: (%v)", err)
		}
		res, err = r.Reconcile(ctx, req)
		if err != nil {
			t.Fatalf("reconcile: (%v)", err)
		}
		if res != (reconcile.Result{}) {
			t.Error("reconcile did not return an empty Result")
		}
		res, err = r.Reconcile(ctx, req)
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
		err = r.Client.Get(context.TODO(), types.NamespacedName{
			Name:      customdomain.Name,
			Namespace: customdomain.Namespace,
		}, customdomain)
		if err != nil {
			t.Fatalf("get failed: (%v)", err)
		}
		log.Info("Deleting customdomain instance")
		customdomain.SetDeletionTimestamp(&now)
		err = r.Client.Update(context.TODO(), customdomain)
		if err != nil {
			t.Fatalf("update failed: (%v)", err)
		}
		res, err = r.Reconcile(ctx, req)
		if err != nil {
			t.Fatalf("reconcile: (%v)", err)
		}
		res, err = r.Reconcile(ctx, req)
		if err != nil {
			t.Fatalf("reconcile: (%v)", err)
		}
		if res != (reconcile.Result{}) {
			t.Error("reconcile did not return an empty Result")
		}
		res, err = r.Reconcile(ctx, req)
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
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Name:      customdomain.Name,
		Namespace: customdomain.Namespace,
	}, customdomain)
	if err != nil {
		t.Fatalf("update failed: (%v)", err)
	}
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	log.Info("Deleting customdomain instance")
	// check the deletion of the customdomain and reconcile path
	err = r.Client.Delete(context.TODO(), customdomain)
	if err != nil {
		t.Errorf("delete customdomain: (%v)", err)
	}
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	if res != (reconcile.Result{}) {
		t.Error("reconcile did not return an empty Result")
	}
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	if res != (reconcile.Result{}) {
		t.Error("reconcile did not return an empty Result")
	}

	// check copied secret
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Name:      instanceName,
		Namespace: ingressNamespace,
	}, actualIngressSecret)
	if err == nil {
		t.Fatalf("secret %s was not deleted!", instanceName)
	}

	// check that ingresscontroller was deleted
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Name:      instanceName,
		Namespace: ingressOperatorNamespace,
	}, actualCustomIngress)
	if err == nil {
		t.Fatalf("ingresscontroller %s was not deleted!", instanceName)
	}
}

// UpdatePlatformStatus gets the infrastructure object "cluster",
// updates its status to populate the PlatformStatus type to AWS
func UpdatePlatformStatus(kclient client.Client) error {

	u := &configv1.Infrastructure{}
	ns := types.NamespacedName{
		Namespace: "",
		Name:      "cluster",
	}
	err := kclient.Get(context.TODO(), ns, u)
	if err != nil {
		return err
	}

	u.Status.PlatformStatus = &configv1.PlatformStatus{
		Type: configv1.AWSPlatformType,
	}

	err = kclient.Status().Update(context.TODO(), u)
	if err != nil {
		return err
	}

	return nil
}

func NewTestMock(t *testing.T, objs ...client.Object) client.Client {
	mock, err := NewMock(objs...)
	if err != nil {
		t.Fatal(err)
	}

	return mock
}

func NewMock(obs ...client.Object) (client.Client, error) {
	s := runtime.NewScheme()

	if err := corev1.AddToScheme(s); err != nil {
		return nil, err
	}

	// Add Openshift operator v1 scheme
	if err := operatorv1.Install(s); err != nil {
		return nil, err
	}

	// Add Openshift operatoringress v1 scheme
	if err := operatoringressv1.Install(s); err != nil {
		return nil, err
	}

	// Add Openshift config v1 scheme
	if err := configv1.Install(s); err != nil {
		return nil, err
	}

	if err := customdomainv1alpha1.AddToScheme(s); err != nil {
		return nil, err
	}

	return fake.NewClientBuilder().WithScheme(s).WithObjects(obs...).Build(), nil
}
