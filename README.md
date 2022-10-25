# Openshift Dedicated Custom Domain Operator

This operator sets up a [new ingresscontroller with custom certificate](https://docs.openshift.com/container-platform/4.5/networking/ingress-operator.html#nw-ingress-setting-a-custom-default-certificate_configuring-ingress) as a day-2 operation.
The public DNS record of this new ingresscontroller can then be used by external DNS to create a wildcard CNAME record to for a custom domain.

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

### Building And Deploying

Also see [Deploying the operator from a development branch for testing](DEPLOYING-FOR-TESTING.md)


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
# deploy manifests
oc apply -f deploy/crds/managed.openshift.io_customdomains.yaml
oc apply -f deploy/
# build
make docker-build docker-push
# update image with image in build output
oc set image -n openshift-custom-domains-operator deployment/custom-domains-operator custom-domains-operator=quay.io/dustman9000/custom-domains-operator:v0.1.29-a48b301e
```


## Testing
See [TESTING](TESTING.md)
