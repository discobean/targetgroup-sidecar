help:
	@fgrep -h "##" $(MAKEFILE_LIST) | fgrep -v fgrep | sed -e 's/\\$$//' | sed -e 's/:.*##/:/'

ensure: ## Run dep ensure
	dep ensure -v -update

build: ensure ## Build a local binary
	go build

build-amd64: ensure ## Build a binary for docker amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o targetgroup-sidecar

docker: build-amd64 ## Build a docker image
	docker build -t targetgroup-sidecar .

push: docker
	docker tag targetgroup-sidecar discobean/targetgroup-sidecar:$(shell git rev-parse HEAD)
	docker tag targetgroup-sidecar discobean/targetgroup-sidecar:latest
	docker push discobean/targetgroup-sidecar:$(shell git rev-parse HEAD)
	docker push discobean/targetgroup-sidecar:latest
