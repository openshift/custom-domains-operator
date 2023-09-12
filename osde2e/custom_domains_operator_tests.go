// DO NOT REMOVE TAGS BELOW. IF ANY NEW TEST FILES ARE CREATED UNDER /osde2e, PLEASE ADD THESE TAGS TO THEM IN ORDER TO BE EXCLUDED FROM UNIT TESTS.
//go:build osde2e
// +build osde2e

package osde2etests

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	customdomainv1alpha1 "github.com/openshift/custom-domains-operator/api/v1alpha1"
	managed "github.com/openshift/custom-domains-operator/controller"
	"github.com/openshift/osde2e-common/pkg/clients/openshift"
	"github.com/openshift/osde2e-common/pkg/gomega/assertions"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = ginkgo.Describe("Custom Domains Operator", ginkgo.Ordered, func() {
	const (
		pollInterval      = 10 * time.Second
		ingressNamespace  = "openshift-ingress"
		testAppName       = "hello-openshift"
		testServiceName   = testAppName + "-service"
		testRouteHostname = testAppName + "-route"
	)

	var (
		k8s                        *openshift.Client
		impersonatedResourceClient *openshift.Client
		testCustomDomain           *customdomainv1alpha1.CustomDomain
		testCustomDomainSecret     *corev1.Secret
		testDomainName             string
		testSecretName             string
		testDnsNames               []string
		testNamespace              *corev1.Namespace
		testNamespaceName          string
		testCustomDomainCRName     string
		testService                *corev1.Service
		testDeployment             *appsv1.Deployment
	)

	ginkgo.BeforeAll(func(ctx context.Context) {
		log.SetLogger(ginkgo.GinkgoLogr)
		var err error
		k8s, err = openshift.New(ginkgo.GinkgoLogr)
		Expect(err).ShouldNot(HaveOccurred(), "Unable to setup k8s client")
		Expect(customdomainv1alpha1.AddToScheme(k8s.GetScheme())).Should(Succeed(), "Unable to register customdomainv1alpha1 api scheme")
		impersonatedResourceClient, err = k8s.Impersonate("test-user@redhat.com", "dedicated-admins")
		Expect(err).ShouldNot(HaveOccurred(), "Unable to setup k8s client")
		Expect(customdomainv1alpha1.AddToScheme(impersonatedResourceClient.GetScheme())).Should(Succeed(), "Unable to register customdomainv1alpha1 api scheme")

		// get clusterversion
		clusterVersionObject := new(configv1.ClusterVersion)
		Expect(k8s.Get(ctx, "version", "", clusterVersionObject)).Should(Succeed(), "Unable to get ClusterVerion")

		usingNewManagedIngress, err := managed.IsUsingNewManagedIngressFeature(k8s.GetControllerRuntimeClient(), ginkgo.GinkgoLogr)
		Expect(err).ShouldNot(HaveOccurred(), "Failed to check for managed ingress feature")

		if managed.IsVersionGreaterOrEqualThan(clusterVersionObject.Status.History[0].Version, "4.13") && usingNewManagedIngress {
			ginkgo.Skip("deprecated on >=4.13: https://github.com/openshift/custom-domains-operator#deprecation")
		}
	})

	// BeforeEach initializes a CustomDomain for testing
	ginkgo.BeforeEach(func(ctx context.Context) {
		specSuffix := strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
		testNamespaceName = "osde2e-" + specSuffix
		testCustomDomainCRName = "osde2e-" + specSuffix
		testDomainName = fmt.Sprintf("%s.io", testCustomDomainCRName)
		testSecretName = testCustomDomainCRName + "-secret"

		ginkgo.By("Working in test namespace " + testNamespaceName)
		testNamespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespaceName}}
		err := k8s.Create(ctx, testNamespace)
		Expect(err).ShouldNot(HaveOccurred(), "Unable to create namespace")

		ginkgo.By("Creating ssl certificate and tls secret")
		testDnsNames := []string{fmt.Sprintf("*.%s", testDomainName)}
		testCustomDomainSecret = makeTlsSecret(ctx, testSecretName, testNamespaceName, testDnsNames)
		err = k8s.Create(ctx, testCustomDomainSecret)
		Expect(err).ShouldNot(HaveOccurred(), "Failed to create secret")

		ginkgo.By("Creating CustomDomain CR")
		testCustomDomain = &customdomainv1alpha1.CustomDomain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testCustomDomainCRName,
				Namespace: testNamespaceName,
			},
			Spec: customdomainv1alpha1.CustomDomainSpec{
				Domain: testDomainName,
				Certificate: corev1.SecretReference{
					Name:      testSecretName,
					Namespace: testNamespaceName,
				},
			},
		}

		err = k8s.Create(ctx, testCustomDomain)
		Expect(err).ShouldNot(HaveOccurred(), "Error creating custom domain")

		ginkgo.By("Waiting for CustomDomain endpoint to be ready")
		Eventually(func() bool {
			if err := k8s.Get(ctx, testCustomDomainCRName, testNamespaceName, testCustomDomain); err != nil {
				return false
			}
			if testCustomDomain.Status.State == "Ready" && testCustomDomain.Status.Endpoint != "" {
				return true
			}
			return false
		}).WithTimeout(5*time.Minute).WithPolling(pollInterval).Should(BeTrue(), "Endpoint never became ready")

		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up setup")
			Expect(k8s.Delete(ctx, testCustomDomain)).Should(Succeed(), "Failed to delete CustomDomain")
			Expect(k8s.Delete(ctx, testCustomDomainSecret)).Should(Succeed(), "Failed to delete secret")
			Expect(k8s.Delete(ctx, testNamespace)).Should(Succeed(), "Failed to delete namespace")
		})
	})

	ginkgo.It("allows dedicated admin to create and expose test app using a CustomDomain", func(ctx context.Context) {
		ginkgo.By("Creating deployment")
		testDeployment = makeDeployment(testAppName, testNamespaceName)
		err := impersonatedResourceClient.Create(ctx, testDeployment)
		Expect(err).ShouldNot(HaveOccurred(), "Unable to create deployment")

		ginkgo.By("Ensuring deployment is up")
		assertions.EventuallyDeployment(ctx, impersonatedResourceClient, testDeployment.Name, testDeployment.Namespace)

		ginkgo.By("Exposing service")
		testService = makeService(testAppName, testServiceName, testNamespaceName)
		err = impersonatedResourceClient.Create(ctx, testService)
		Expect(err).ShouldNot(HaveOccurred(), "Unable to create service %s/%s", testNamespaceName, testService.Name)

		ginkgo.By("Creating openshift route using CustomDomain hostname")
		testRoute := makeRoute(testRouteHostname, testServiceName, testNamespaceName, testCustomDomain)
		err = impersonatedResourceClient.Create(ctx, testRoute)
		Expect(err).ShouldNot(HaveOccurred(), "Unable to create route")

		ginkgo.By("Pinging the app using CustomDomain route")
		err = k8s.Get(ctx, testCustomDomainCRName, testNamespaceName, testCustomDomain)
		Expect(err).ShouldNot(HaveOccurred(), "Could not get custom domain instance")
		endpointClient := getCustomClient(testRoute, testCustomDomain)
		Eventually(func() (*http.Response, error) {
			response, err := endpointClient.Get("https://" + testRoute.Spec.Host)
			if err != nil {
				return nil, err
			}
			return response, nil
		}).WithTimeout(10*time.Minute).WithPolling(pollInterval).Should(HaveHTTPStatus(http.StatusOK), "Test app route never responded")
	})

	ginkgo.It("allows dedicated admin to replace certificates on CustomDomain", func(ctx context.Context) {
		origIngressSecret := &corev1.Secret{}
		err := k8s.Get(ctx, testCustomDomainCRName, ingressNamespace, origIngressSecret)
		Expect(err).ShouldNot(HaveOccurred(), "Failed to get secret")
		err = impersonatedResourceClient.Get(ctx, testCustomDomainCRName, testNamespaceName, testCustomDomain)
		Expect(err).ShouldNot(HaveOccurred(), "Could not get custom domain instance")

		ginkgo.By("Generating a new certificate")
		newSecretName := fmt.Sprintf("%s-new-secret", testCustomDomainCRName)
		newSecret := makeTlsSecret(ctx, newSecretName, testNamespaceName, testDnsNames)
		err = k8s.Create(ctx, newSecret)
		Expect(err).ShouldNot(HaveOccurred(), "Could not create new secret")

		ginkgo.By("Replacing the certificate in the customdomain CR")
		testCustomDomain.Spec.Certificate.Name = newSecret.Name
		testCustomDomain.Spec.Certificate.Namespace = newSecret.Namespace
		err = impersonatedResourceClient.Update(ctx, testCustomDomain)
		Expect(err).ShouldNot(HaveOccurred(), "Could not update custom domain with new secret")

		ginkgo.By("Verifying CD ingress secret matches the new tls secret")
		currentIngressSecret := &corev1.Secret{}
		Eventually(func() bool {
			err := k8s.Get(ctx, testCustomDomainCRName, ingressNamespace, currentIngressSecret)
			if err != nil || bytes.Equal(currentIngressSecret.Data["tls.crt"], origIngressSecret.Data["tls.crt"]) {
				return false
			}
			return true
		}).WithTimeout(5*time.Minute).WithPolling(pollInterval).Should(BeTrue(), "TLS cert change never took effect")
	})
})

func makeTlsSecret(ctx context.Context, secretName string, testNamespaceName string, dnsNames []string) *corev1.Secret {
	ginkgo.GinkgoHelper()
	customDomainRSAKey, err := rsa.GenerateKey(rand.Reader, 4096)
	Expect(err).ShouldNot(HaveOccurred(), "Failed to generate key")
	customDomainX509Template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:       []string{"Red Hat, Inc"},
			OrganizationalUnit: []string{"Openshift Dedicated End-to-End Testing"},
		},
		DNSNames:              dnsNames,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(0, 0, 1),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	customDomainX509, err := x509.CreateCertificate(rand.Reader, customDomainX509Template, customDomainX509Template, &customDomainRSAKey.PublicKey, customDomainRSAKey)
	Expect(err).ShouldNot(HaveOccurred(), "Failed to create cert")
	secretData := make(map[string][]byte)
	secretData["tls.key"] = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(customDomainRSAKey),
	})
	secretData["tls.crt"] = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: customDomainX509,
	})
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: testNamespaceName,
		},
		Type: corev1.SecretTypeTLS,
		Data: secretData,
	}
}

func makeDeployment(testAppName, testNamespaceName string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testAppName,
			Namespace: testNamespaceName,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"deployment": testAppName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"deployment": testAppName},
				}, Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  testAppName,
							Image: "docker.io/openshift/" + testAppName,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8080,
								},
								{
									ContainerPort: 8888,
								},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: pointer.Bool(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
								RunAsNonRoot: pointer.Bool(true),
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
						},
					},
				},
			},
		},
	}
}

func makeService(testAppName, testServiceName, testNamespaceName string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testServiceName,
			Namespace: testNamespaceName,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "8080-tcp",
					Protocol: corev1.ProtocolTCP,
					Port:     8080,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 8080,
					},
				},
				{
					Name:     "8888-tcp",
					Protocol: corev1.ProtocolTCP,
					Port:     8888,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 8888,
					},
				},
			},
			Selector: map[string]string{"deployment": testAppName},
			Type:     corev1.ServiceTypeClusterIP,
		},
	}
}

func makeRoute(testRouteHostname, testServiceName, testNamespaceName string, testCustomDomain *customdomainv1alpha1.CustomDomain) *routev1.Route {
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testRouteHostname,
			Namespace: testNamespaceName,
		},
		Spec: routev1.RouteSpec{
			Host: testRouteHostname + "." + testCustomDomain.Spec.Domain,
			Port: &routev1.RoutePort{
				TargetPort: intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "8080-tcp",
				},
			},
			TLS: &routev1.TLSConfig{
				Termination: routev1.TLSTerminationEdge,
			},
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: testServiceName,
			},
		},
	}
}

// Returns http client using dialContext customized for http client to simulate DNS lookup.
// DialContext redirects requests to customdomain endpoint instead of DNS record, since pinging DNS would require CNAME record setup in DNS provider.
func getCustomClient(testRoute *routev1.Route, testCustomDomain *customdomainv1alpha1.CustomDomain) *http.Client {
	customDialer := &net.Dialer{
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: 10 * time.Second,
				}
				return d.DialContext(ctx, network, address)
			},
		},
	}
	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == testRoute.Spec.Host+":443" {
			addr = testCustomDomain.Status.Endpoint + ":443"
		}
		return customDialer.DialContext(ctx, network, addr)
	}
	http.DefaultTransport.(*http.Transport).DialContext = dialContext
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			DialContext: dialContext,
		},
	}
}
