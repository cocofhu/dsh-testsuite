DSH_VERSION ?= 0.1.0-rc.8
IMAGE_REPO ?= dsh-testsuite-runtime

.PHONY: test image run tidy

test:
	go vet ./...
	go test ./...
	python3 image/common/allow_builds_test.py

tidy:
	go mod tidy

image:
	@test -f image/$(DSH_VERSION)/Dockerfile || { echo "missing image/$(DSH_VERSION)/Dockerfile"; exit 1; }
	docker build --build-arg DSH_VERSION=$(DSH_VERSION) \
	  --label dsh-testsuite.runtime=1 \
	  --label dsh-testsuite.dsh-version=$(DSH_VERSION) \
	  -f image/$(DSH_VERSION)/Dockerfile \
	  -t $(IMAGE_REPO):$(DSH_VERSION) \
	  ./image

run:
	go run ./cmd/dsh-testsuite -config config.example.yaml
