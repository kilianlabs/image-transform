.PHONY: check check-project check-local format openapi openapi-check client-check build smoke smoke-run smoke-platforms

PLATFORMS ?= linux/amd64 linux/arm64
PLATFORM_FLAG = $(if $(PLATFORM),--platform $(PLATFORM))

check: check-project
	$(MAKE) smoke

check-project:
	docker build --target check -t image-transform:check .

# Run inside the development image, or on a host with Go and libvips development libraries.
check-local:
	test -z "$$(gofmt -l cmd internal)"
	test -z "$$(gofmt -l scripts/clientcheck_test.go.txt)"
	go vet ./...
	go test -race -v ./...
	go build ./...
	$(MAKE) openapi-check client-check

format:
	gofmt -w cmd internal scripts/clientcheck_test.go.txt

openapi:
	CGO_ENABLED=0 go run ./cmd/openapi -write openapi.json

openapi-check:
	CGO_ENABLED=0 go run ./cmd/openapi -check openapi.json

client-check:
	sh scripts/check-client.sh

build:
	docker build --target runtime -t image-transform:local .

smoke:
	docker buildx build $(PLATFORM_FLAG) --target smoke --load -t image-transform:smoke .
	$(MAKE) smoke-run

smoke-run:
	docker run --rm $(PLATFORM_FLAG) --read-only \
		--tmpfs /tmp:rw,noexec,nosuid,size=2147483648 \
		--memory=2g --cpus=2 --pids-limit=128 image-transform:smoke

smoke-platforms:
	@set -e; for platform in $(PLATFORMS); do \
		$(MAKE) smoke PLATFORM=$$platform; \
	done
