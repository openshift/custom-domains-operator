# Testing

## Unit Testing

```
go test ./pkg/controller/customdomain/ -coverprofile /tmp/cp.out && go tool cover -html /tmp/cp.out
```

## Live Testing
### SRE Setup
1. [Pause Syncset](https://github.com/openshift/ops-sop/blob/master/v4/knowledge_base/pause-syncset.md)
2. [Elevate Privleges](https://github.com/openshift/ops-sop/blob/master/v4/howto/backplane-elevate-privileges.md)

Create CRD
```
oc apply -f deploy/crds/managed.openshift.io_customdomains_crd.yaml
```

#### Ensure operator is running
operator-sdk run --local --namespace ''
OR
oc create namespace openshift-custom-domains-operator
oc apply -f deploy/

### Cert Setup

#### Let's Encrypt Notes
If you do not have a wildcard certificate for the custom domain, you can use Let's Encrypt (certbot) to generate a wildcard certificate.

Install certbot and obtain wildcard cert:
```
brew install certbot
sudo certbot certonly --manual --preferred-challenges=dns --agree-tos --email=<your-email> -d '*.apps.<domain>'
```
Follow instructions to verify domain ownership in Route53 (or other DNS vendor).

### Add Secret and CustomDomain CR
To generate a self signed cert and key follow these [steps](https://www.linode.com/docs/guides/create-a-self-signed-tls-certificate/).

Example of creating a secret and customdomain:
```
oc new-project my-project
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
    namespace: my-project
")
```

### Test Custom Apps Domain

#### Get DNS Record from CR
Example:
```
oc get customdomain acme -o json | jq -r .status.dnsRecord
*.acme.cluster01.x8s0.s1.openshiftapps.com
```

#### Setup External DNS with CNAME record
If you don't want to update the DNS vendor, skip to the ["Testing without DNS vendor updates"](#testing-without-dns-vendor-updates) section.

Example:
```
*.apps.acme.io -> _dns.acme.cluster01.x8s0.s1.openshiftapps.com
```

#### Create and Test App
Example:
```
oc new-app --docker-image=docker.io/openshift/hello-openshift
$ oc create route edge --service=hello-openshift hello-openshift-tls --hostname hello-openshift-tls-my-project.apps.acme.io
$ curl https://hello-openshift-tls-my-project.apps.acme.io
Hello OpenShift!
```

#### Testing without External DNS Updates (Option B)

Example for creating an app and a route:

```
oc new-app --docker-image=docker.io/openshift/hello-openshift -n my-project
oc create route edge -n my-project --service=hello-openshift hello-openshift-tls --hostname hello-openshift-tls-my-project.apps.acme.io
```
To find the IP of the endpoint use this command: 

```
dig +short $(oc get customdomain acme -o json | jq -r .status.endpoint) 
```
To test the app:
```
curl -k https://hello-openshift-tls-my-project.stuff.alice.io --resolve hello-openshift-tls-my-project.apps.acme.io:443:<IP of the endpoint>
Hello OpenShift!
```