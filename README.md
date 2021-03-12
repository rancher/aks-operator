# Rancher/aks-operator

Rancher aks-operator is a new service, which takes care about Azure Kubernetes Service cluster provisioning for Rancher based on AKSClusterConfig defined by Custom Resource Definition. 

## Latest TAG version 

`TAG=v1.0-rc1 make`

## To build and test aks-operator, please go with following procedure:

### 1. Checkout aks-operator repository

`git checkout https://github.com/rancher/aks-operator`

### 2. Download required modules and build project

`cd aks-operator`

`go build -o aks-operator main.go`

### 3. Deploy CRD AKSClusterConfig on K8s

`export KUBECONFIG=<kubeconfig_path>`

`kubectl apply -f crds/aksclusterconfig.yaml`


### 4. Edit AKSClusterConfig setup

`cp examples/create-example.yaml examples/create-aks.yaml`

`vim examples/create-aks.yaml`

### 5. Create AKS secret with clientID and clientSecret

`export REPLACE_WITH_K8S_SECRETS_NAME=aks-secret`

`kubectl create secret generic $REPLACE_WITH_K8S_SECRETS_NAME --from-literal=azurecredentialConfig-subscriptionId=<REPLACE_WITH_SUBSCRIPTIONID> --from-literal=azurecredentialConfig-clientId=<REPLACE_WITH_CLIENTID> --from-literal=azurecredentialConfig-clientSecret=<REPLACE_WITH_CLIENTSECRET>`

### 6. Start aks-operator

`./aks-operator`

### 7. Apply AKSClusterConfig

`kubectl apply -f examples/create-aks.yaml`

### 8. Watch the logs

### 9. To delete AKS cluster

`kubectl delete -f examples/create-aks.yaml`

