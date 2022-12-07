# rancher/aks-operator

Rancher aks-operator is a Kubernetes CRD controller that controls cluster provisioning in Azure Kubernetes Service using an AKSClusterConfig defined by a Custom Resource Definition.

## Latest TAG version 

    TAG=v1.0-rc1 make

## Testing the AKS Operator (Standalone)

### Clone the repository

    git clone https://github.com/rancher/aks-operator

### Download required modules and build project

    cd aks-operator
    go build -o aks-operator main.go

### Deploy AKSClusterConfig CRD

    export KUBECONFIG=<kubeconfig_path>
    kubectl apply -f crds/aksclusterconfig.yaml

### Edit AKSClusterConfig setup

    cp examples/create-example.yaml examples/create-aks.yaml
    vim examples/create-aks.yaml

### Create AKS secret with clientID and clientSecret

    export REPLACE_WITH_K8S_SECRETS_NAME=aks-secret
    kubectl create secret generic $REPLACE_WITH_K8S_SECRETS_NAME --from-literal=azurecredentialConfig-subscriptionId=<REPLACE_WITH_SUBSCRIPTIONID> --from-literal=azurecredentialConfig-clientId=<REPLACE_WITH_CLIENTID> --from-literal=azurecredentialConfig-clientSecret=<REPLACE_WITH_CLIENTSECRET>

### Start the AKS operator

    ./aks-operator

###  Apply AKSClusterConfig CRD

    kubectl apply -f examples/create-aks.yaml

### Watch the logs

### Delete the AKS cluster

    kubectl delete -f examples/create-aks.yaml

## Developing the AKS Operator (on Rancher)

The easiest way to debug and develop the AKS operator is to replace the default operator on a running Rancher instance with your local one.

* Run a local Rancher server
* Provision an AKS cluster
* Scale the aks-operator deployment to replicas=0 in the Rancher UI
* Open the aks-operator repo in Goland, set `KUBECONFIG=<kubeconfig_path>` in Run Configuration Environment 
* Run the aks-operator in Debug Mode
* Set breakpoints

## Release Process

### When should I release?

A KEv2 operator should be released if 

* There have been several commits since the last release,
* You need to pull in an update/bug fix/backend code to unblock UI for a feature enhancement in Rancher
* The operator needs to be unRC for a Rancher release

### How do I release?

* Tag the latest commit on the `master` branch. For example, if latest tag is `v1.0.8-rc1` you would tag `v1.0.8-rc2`.


    git pull upstream master --tags     // get the latest upstream changes (not your fork)
    git tag v1.0.8-rc2                  // tag HEAD
    git push upstream v1.0.8-rc2        // push the tag

* Submit a [rancher/charts PR](https://github.com/rancher/charts/pull/2242) to update the operator and operator-crd chart versions
* Submit a [rancher/rancher PR](https://github.com/rancher/rancher/pull/39745) to update the bundled chart

### How do I unRC?

An unRC is the process of removing the rc from a KEv2 operator tag and means the released version is stable and ready for use.

UnRC is the same process to release a KEv2 operator but instead of bumping the rc you remove the rc. For example, if the latest release of AKS operator is `v1.0.8-rc1`, to unRC you have to release the next version without the rc which would be `v1.0.9`.  

