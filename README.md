# Openshift Dedicated Custom Domain Operator

This allows for a custom domain and certificate to be installed as a day-2 operation.

### Prerequisites

What things you need to install the software and how to install them

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
```
operator-sdk build quay.io/acme/custom-domain-operator
docker push quay.io/acme/custom-domain-operator
sed -i 's|REPLACE_ME|quay.io/acme|g' deploy/operator.yaml
oc apply -f deploy/operator.yaml
```

#### Add Secrets and CustomDomain CRD
```
oc new-project acme-apps
# secret must be created in the 'openshift-ingress' namespace
oc -n openshift-ingress create secret tls acme-tls --cert=fullchain.pem --key=privkey.pem
oc apply -f <(echo "
apiVersion: managed.openshift.io/v1alpha1
kind: CustomDomain
metadata:
  name: cluster
spec:
  domain: apps.acme.io
  tlsSecret:
    name: acme-tls
    namespace: acme-apps
")
```
