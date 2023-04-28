# rancher/aks-operator

AKS operator is a Kubernetes CRD controller that controls cluster provisioning in Azure Kubernetes Service using an AKSClusterConfig defined by a Custom Resource Definition.

## Build

Operator binary can be built using the following command:

```bash
    make operator
```

## Deploy operator from source

You can use the following command to deploy a Kind cluster with Rancher manager and operator:

```bash
    make kind-deploy-operator
```

After this, you can also downscale operator deployment and run operator from a local binary.

## Tests

Running unit tests can be done using the following command:

```bash
    make test
```

For running e2e set the following variables and run:

```bash
    export AZURE_CLIENT_ID="replace_with_your_client_id"
    export AZURE_CLIENT_SECRET="replace_with_client_secret"
    export AZURE_SUBSCRIPTION_ID="replace_with_subscription_id"
    make kind-e2e-tests
```

A Kind cluster will be created, and the e2e tests will be run against it.

## Release

#### When should I release?

A KEv2 operator should be released if

* There have been several commits since the last release,
* You need to pull in an update/bug fix/backend code to unblock UI for a feature enhancement in Rancher
* The operator needs to be unRC for a Rancher release

#### How do I release?

Tag the latest commit on the `master` branch. For example, if latest tag is `v1.0.8-rc1` you would tag `v1.0.8-rc2`.

```bash
    git pull upstream master --tags     // get the latest upstream changes (not your fork)
    git tag v1.0.8-rc2                  // tag HEAD
    git push upstream v1.0.8-rc2        // push the tag
```

After pushing the release tag, you need to run 2 Github actions. You can find them in the Actions tab of the repo:

* `Update AKS operator in rancher/rancher` - This action will update the AKS operator in rancher/rancher repo. It will bump go dependencies.
* `Update AKS Operator in rancher/charts` - This action will update the AKS operator in rancher/charts repo. It will bump the chart version.

#### How do I unRC?

UnRC is the process of removing the rc from a KEv2 operator tag and means the released version is stable and ready for use. Release the KEv2 operator but instead of bumping the rc, remove the rc. For example, if the latest release of AKS operator is `v1.0.8-rc1`, release the next version without the rc which would be `v1.0.9`.
