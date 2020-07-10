# Testing

## Unit Testing

```
go test ./pkg/controller/customdomain/ -coverprofile /tmp/cp.out && go tool cover -html /tmp/cp.out
```

## Live Testing
### SRE Setup
[Pause Syncset](https://github.com/openshift/ops-sop/blob/master/v4/howto/pause-syncset.md)
[Elevate Privleges](https://github.com/openshift/ops-sop/blob/master/v4/howto/manage-privileges.md#elevate-privileges)

Create CRD
```
oc apply -f deploy/crds/managed.openshift.io_customdomains_crd.yaml
```

#### Ensure operator is running
operator-sdk run --local --namespace ''
OR
oc create namespace openshift-custom-domains-operator
oc apply -f deploy/

### Customer Setup

#### Setup CNAME Record
Get cluster FQDN from API
```
oc cluster-info
```

1. Go to AWS (or other DNS Vendor)
2. Update CNAME record in Route53 (or other DNS Vendor)

Verify
```
oc get svc -n openshift-ingress router-default
dig +short demo.apps.fidata.io
dig +short demo.apps.drow-dev02.p1p4.s1.devshift.org
```

#### Setup TLS Secret

##### Certbot
Install certbot and obtain wildcard cert
```
brew install certbot
sudo certbot certonly --manual --preferred-challenges=dns --agree-tos --email=<your-email> -d '*.apps.<domain>'
```
Follow instructions to verify domain ownership in Route53 (or other DNS vendor).

##### Create Secret
oc create secret -n openshift-ingress tls custom-default-tls --cert=/etc/letsencrypt/live/apps.<domain>/fullchain.pem --key=/etc/letsencrypt/live/apps.<domain>/privkey.pem

### Create Custom Domain
oc apply -f <(echo "
apiVersion: managed.openshift.io/v1alpha1
kind: CustomDomain
metadata:
  name: cluster
spec:
  domain: apps.<domain>
  tlsSecret: custom-default-tls
")


### Test

Verify pods running
```
oc get pods -n openshift-ingress
```

Verify operators are not degraded
```
oc get clusteroperators
```

Check routes
```
oc get routes --all-namespaces
```

#### Test Application
```
oc new-project test
oc new-app --docker-image=docker.io/openshift/hello-openshift
oc get pods
oc create route edge --service=hello-openshift hello-openshift-tls
curl https://hello-openshift-tls-test.apps.<domain>
```


#### Test Restoring Domain
```
oc delete customdomain cluster
oc get routes --all-namespaces
oc get pods -n openshift-ingress
```

## Check Affected CRs
```
oc get ingresses.config/cluster -o yaml
oc get dnses.operator/default -o yaml
oc get dnses.config/cluster -o yaml
oc get ingresscontrollers/default -n openshift-ingress-operator -o yaml
oc get publishingstrategies.cloudingress.managed.openshift.io/publishingstrategy -n openshift-cloud-ingress-operator -o yaml
oc get routes -A
```
