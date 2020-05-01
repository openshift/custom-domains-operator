package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
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

	// TLSSecret points to the secret where the TLS secret should be stored once generated.
	TLSSecret corev1.ObjectReference `json:"tlsSecret"`
}

// CustomDomainStatus defines the observed state of CustomDomain
type CustomDomainStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CustomDomain is the Schema for the customdomains API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=customdomains,scope=Cluster
type CustomDomain struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CustomDomainSpec   `json:"spec,omitempty"`
	Status CustomDomainStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CustomDomainList contains a list of CustomDomain
type CustomDomainList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CustomDomain `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CustomDomain{}, &CustomDomainList{})
}
