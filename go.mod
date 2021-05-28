module github.com/rancher/aks-operator

go 1.16

replace k8s.io/client-go => k8s.io/client-go v0.18.0

require (
	github.com/Azure/azure-sdk-for-go v50.0.1-0.20210114072321-4a06a7dc9c3c+incompatible
	github.com/Azure/go-autorest/autorest v0.11.16
	github.com/Azure/go-autorest/autorest/adal v0.9.11-0.20210111195520-9fc88b15294e
	github.com/Azure/go-autorest/autorest/to v0.4.1-0.20210111195520-9fc88b15294e
	github.com/Azure/go-autorest/autorest/validation v0.3.2-0.20210111195520-9fc88b15294e // indirect
	github.com/rancher/lasso v0.0.0-20200905045615-7fcb07d6a20b
	github.com/rancher/wrangler v0.7.3-0.20201020003736-e86bc912dfac
	github.com/rancher/wrangler-api v0.6.1-0.20200427172631-a7c2f09b783e
	github.com/sirupsen/logrus v1.4.2
	golang.org/x/crypto v0.0.0-20210513164829-c07d793c2f9a // indirect
	k8s.io/api v0.18.8
	k8s.io/apimachinery v0.18.8
	k8s.io/client-go v0.18.8
)
