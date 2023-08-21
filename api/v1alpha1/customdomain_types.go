package v1alpha1

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CustomDomainSpec defines the desired state of CustomDomain
type CustomDomainSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html

	// This field can be used to define the custom domain
	Domain string `json:"domain"`

	// Certificate points to the custom TLS secret
	Certificate corev1.SecretReference `json:"certificate"`

	// This field determines whether the CustomDomain ingress is internal or external. Defaults to External if empty.
	//
	// +kubebuilder:validation:Enum=External;Internal
	// +kubebuilder:default:="External"
	// +optional
	Scope string `json:"scope,omitempty"`

	// This field is used to filter the set of namespaces serviced by the
	// CustomDomain ingress. This is useful for implementing shards.
	//
	// If unset, the default is no filtering.
	//
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// This field is used to filter the set of Routes serviced by the ingress
	// controller. This is useful for implementing shards.
	//
	// If unset, the default is no filtering.
	//
	// +optional
	RouteSelector *metav1.LabelSelector `json:"routeSelector,omitempty"`

	// This field is used to specify the type of AWS load balancer.
	//
	// Valid values are:
	//
	// * "Classic": A Classic Load Balancer that makes routing decisions at either the transport layer (TCP/SSL) or the application layer (HTTP/HTTPS). See the following for additional details: https://docs.aws.amazon.com/AmazonECS/latest/developerguide/load-balancer-types.html#clb
	//
	// * "NLB": A Network Load Balancer that makes routing decisions at the transport layer (TCP/SSL). See the following for additional details: https://docs.aws.amazon.com/AmazonECS/latest/developerguide/load-balancer-types.html#nlb
	//
	// +kubebuilder:validation:Enum=Classic;NLB
	// +kubebuilder:default:="Classic"
	// +optional
	LoadBalancerType operatorv1.AWSLoadBalancerType `json:"loadBalancerType,omitempty"`
}

// CustomDomainStatus defines the observed state of CustomDomain
type CustomDomainStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html

	// The various conditions for the custom domain
	Conditions []CustomDomainCondition `json:"conditions"`

	// The overall state of the custom domain
	State CustomDomainStateType `json:"state,omitempty"`

	// The DNS record added for the ingress controller
	DNSRecord string `json:"dnsRecord"`

	// The endpoint is a resolvable DNS address for external DNS to point to
	Endpoint string `json:"endpoint"`

	// The scope dictates whether the ingress controller is internal or external
	// +optional
	Scope string `json:"scope"`
}

// CustomDomainStateType is a valid value for CustomDomainStatus.State
type CustomDomainStateType string

const (
	// CustomDomainStateNotReady is set when custom domain is not ready
	CustomDomainStateNotReady CustomDomainStateType = "NotReady"

	// CustomDomainStateReady is set when a custom domain is ready
	CustomDomainStateReady CustomDomainStateType = "Ready"
)

// +kubebuilder:object:root=true

// CustomDomain is the Schema for the customdomains API
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.status.endpoint`
// +kubebuilder:printcolumn:name="Domain",type=string,JSONPath=`.spec.domain`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.state`
// +kubebuilder:resource:path=customdomains,scope=Cluster
type CustomDomain struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CustomDomainSpec   `json:"spec,omitempty"`
	Status CustomDomainStatus `json:"status,omitempty"`
}

// CustomDomainCondition contains details for the current condition of a custom domain
type CustomDomainCondition struct {
	// Type is the type of the condition.
	Type CustomDomainConditionType `json:"type,omitempty"`
	// Status is the status of the condition
	Status corev1.ConditionStatus `json:"status,omitempty"`
	// LastProbeTime is the last time we probed the condition.
	// +optional
	LastProbeTime metav1.Time `json:"lastProbeTime,omitempty"`
	// LastTransitionTime is the laste time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// Reason is a unique, one-word, CamelCase reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// Message is a human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty"`
}

// CustomDomainConditionType is a valid value for CustomDomainCondition.Type
type CustomDomainConditionType string

const (
	// CustomDomainConditionCreating is set when a CustomDomain is being created
	CustomDomainConditionCreating CustomDomainConditionType = "Creating"

	// CustomDomainConditionDeprecated is set when a CustomDomain has returned to native ingress controller
	CustomDomainConditionDeprecated CustomDomainConditionType = "Deprecated"

	// CustomDomainConditionSecretNotFound is set when the TLS secret has not been found yet
	CustomDomainConditionSecretNotFound CustomDomainConditionType = "SecretNotFound"

	// CustomDomainConditionInvalidName is set when the CR name is invalid (eg. "default", "apps2")
	CustomDomainConditionInvalidName CustomDomainConditionType = "InvalidName"

	// CustomDomainConditionInvalidScope is set when the loadbalancer scope is modified
	CustomDomainConditionInvalidScope CustomDomainConditionType = "InvalidScope"

	// CustomDomainConditionFailed is set when custom domain creation has failed
	CustomDomainConditionFailed CustomDomainConditionType = "Failed"

	// CustomDomainConditionReady is set when a CustomDomain creation is ready
	CustomDomainConditionReady CustomDomainConditionType = "Ready"
)

// +kubebuilder:object:root=true

// CustomDomainList contains a list of CustomDomain
type CustomDomainList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CustomDomain `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CustomDomain{}, &CustomDomainList{})
}
