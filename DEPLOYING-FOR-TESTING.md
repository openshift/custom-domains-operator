# Deploying the operator for testing

This section describess testing your local changes of the custom domains operator. 
First, create an OSD cluster in your staging environment.


### Building and pushing image in Quay

To deploy the operator from your branch, you have to create an image in Quay under your personal account. For example, this will build the image in quay and push it to a personal quay account **foobar** and to a repository named **cdo**  with a tag of **0**.

```
docker build . -f build/Dockerfile -t quay.io/ahubenko/cdo:0
```

```
docker push quay.io/aliceh/cdo:0 
```

### Pause the sync sets for your cluster

To pause sync sets for your cluster, log in to hive shard and run this command.

```
oc annotate clusterdeployment -n <Namespace of cluster deployment>  <Name of cluster> hive.openshift.io/syncset-pause="true"
```
Example:

```
oc annotate clusterdeployment -n uhc-staging-1ve65e4tm86klpn3b12rhg0kvtdkcrm   my-cluster-name hive.openshift.io/syncset-pause="true"
```
### Delete custom domains crd, subscription, csv and installplan

Log in to your cluster and delete the following resources:


```
oc delete crd customdomains.managed.openshift.io  --as backplane-cluster-admin
```

```
oc delete subscription custom-domains-operator -n openshift-custom-domains-operator                                             
```

```
oc get csv -n openshift-custom-domains-operator        
NAME                                               DISPLAY                           VERSION           REPLACES                                           PHASE
configure-alertmanager-operator.v0.1.464-079e35b   configure-alertmanager-operator   0.1.464-079e35b   configure-alertmanager-operator.v0.1.462-e992609   Succeeded
custom-domains-operator.v0.1.135-be33641           custom-domains-operator           0.1.135-be33641   custom-domains-operator.v0.1.134-f17bc00           Succeeded
route-monitor-operator.v0.1.450-6e98c37            Route Monitor Operator            0.1.450-6e98c37   route-monitor-operator.v0.1.448-b25b8ee            Succeeded

oc delete csv custom-domains-operator.v0.1.135-be33641 -n openshift-custom-domains-operator        
```

```
oc delete installplan install-8j94q install-jn7h8 -n openshift-custom-domains-operator 
```

#### Deploy the custom domains operator

In deploy/04_operator.yaml 
```
apiVersion: apps/v1
kind: Deployment
metadata:
  name: custom-domains-operator
  namespace: openshift-custom-domains-operator
spec:
  replicas: 1
  selector:
    matchLabels:
      name: custom-domains-operator
  template:
    metadata:
      labels:
        name: custom-domains-operator
    spec:
      affinity:
        nodeAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - preference:
              matchExpressions:
              - key: node-role.kubernetes.io/infra
                operator: Exists
            weight: 1
      tolerations:
        - effect: NoSchedule
          key: node-role.kubernetes.io/infra
          operator: Exists
      serviceAccountName: custom-domains-operator
      containers:
        - name: custom-domains-operator
          image: REPLACE_ME/custom-domains-operator:latest
          command:
          - custom-domains-operator
          args:
            - "--zap-log-level=debug"
            - "--zap-encoder=console"
          imagePullPolicy: Always
          env:
            - name: WATCH_NAMESPACE
              value: ""
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: OPERATOR_NAME
              value: "custom-domains-operator"
```
replace the line 

```
image: REPLACE_ME/custom-domains-operator:latest
```
with the actual location of the image in quay, for example

```
image: quay.io/aliceh/cdo:0
```
Finally, deploy the contentes of the deploy directory:
```
oc apply -f deploy/
```



