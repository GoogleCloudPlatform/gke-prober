# Image URL to use all building/pushing image targets
IMG ?= gke-prober:latest

# Run tests
test: fmt vet
	go test ./... -coverprofile cover.out

# Build
build: fmt vet
	go build -o gke-prober cmd/*.go

# Run
run: fmt vet
	go run cmd/main.go

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Build the docker image
docker-build: test
	docker build . -t ${IMG}

# Push the docker image
docker-push:
	docker push ${IMG}
