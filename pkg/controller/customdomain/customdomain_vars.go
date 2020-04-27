package customdomain

// system routes (name : namespace)
// TODO: make this configurable in CRD
var systemRoutes = map[string]string{
	"oauth-openshift": "openshift-authentication",
	"console": "openshift-console",
	"downloads": "openshift-console",
	"default-route": "openshift-image-registry",
	"alertmanager-main": "openshift-monitoring",
	"grafana": "openshift-monitoring",
	"prometheus-k8s": "openshift-monitoring",
	"thanos-querier": "openshift-monitoring",
}
