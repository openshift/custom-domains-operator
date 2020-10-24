# Openshift Dedicated Custom Domain Operator

This allows for a custom domain with custom certificate to be installed as a day-2 operation.

### Prerequisites

GVM (GoLang 1.13.6)
```
bash < <(curl -s -S -L https://raw.githubusercontent.com/moovweb/gvm/master/binscripts/gvm-installer)
gvm install go1.13.6
gvm use go1.13.6
```

Operator-SDK
```
wget https://github.com/operator-framework/operator-sdk/releases/download/v0.16.0/operator-sdk-v0.16.0-x86_64-apple-darwin
sudo mv operator-sdk-v0.16.0-x86_64-apple-darwin /usr/local/bin/operator-sdk
sudo chmod a+x /usr/local/bin/operator-sdk
```

### Building And Deploying And Testing

#### Setup
Create Custom Resource Definition (CRD)
```
oc apply -f deploy/crds/managed.openshift.io_customdomains_crd.yaml
```

#### Run locally outside of cluster
```
operator-sdk run --local --namespace ''
```

#### Build and Deploy To Cluster
Choose public container registry e.g. 'quay.io/acme'.
Build and push the image, then update the operator deployment manifest.

Example:
```
oc apply -f deploy/crds/managed.openshift.io_customdomains_crd.yaml
oc apply -f deploy/
IMAGE_REPOSITORY=<your quay org/user> make docker-build docker-push
oc set image -n openshift-custom-domains-operator deployment/custom-domains-operator custom-domains-operator=quay.io/dustman9000/custom-domains-operator:v0.1.29-a48b301e
```

#### Add Secrets and CustomDomain CRD

Example:
```
oc new-project acme-apps
oc create secret tls acme-tls --cert=fullchain.pem --key=privkey.pem
oc apply -f <(echo "
apiVersion: managed.openshift.io/v1alpha1
kind: CustomDomain
metadata:
  name: acme
spec:
  domain: apps.acme.io
  certificate:
    name: acme-tls
    namespace: acme-apps
")
```
