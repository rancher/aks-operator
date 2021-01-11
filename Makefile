SEVERITIES = HIGH,CRITICAL

.PHONY: all
all:
	docker build --build-arg TAG=$(TAG) -t rancher/aks-operator:$(TAG) .

.PHONY: image-push
image-push:
	docker push rancher/aks-operator:$(TAG) >> /dev/null

.PHONY: scan
image-scan:
	trivy --severity $(SEVERITIES) --no-progress --skip-update --ignore-unfixed rancher/aks-operator:$(TAG)

.PHONY: image-manifest
image-manifest:
	docker image inspect rancher/aks-operator:$(TAG)
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest create rancher/aks-operator:$(TAG) \
		$(shell docker image inspect rancher/aks-operator:$(TAG) | jq -r '.[] | .RepoDigests[0]')
