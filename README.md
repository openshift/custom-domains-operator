# Openshift Dedicated Custom Domain Operator

This allows for a custom domain and certificate to be installed as a day-2 operation.

## Getting Started

These instructions will get you a copy of the project up and running on your local machine for development and testing purposes. See deployment for notes on how to deploy the project on a live system.

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

### Deploying

#### Build And Push Docker Image
```
operator-sdk build dustman9000/custom-domain-operator
docker push dustman9000/custom-domain-operator
```

#### Deploy
```
oc apply -f deploy/operator.yaml
```
