#!/bin/sh
set -eu

# renovate: datasource=go depName=github.com/oapi-codegen/oapi-codegen/v2 versioning=go-mod
OAPI_CODEGEN_VERSION=v2.8.2
# renovate: datasource=go depName=github.com/oapi-codegen/runtime versioning=go-mod
OAPI_RUNTIME_VERSION=v1.8.0
# renovate: datasource=go depName=github.com/getkin/kin-openapi versioning=go-mod
KIN_OPENAPI_VERSION=v0.153.0

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
go_version=$(go list -m -f '{{.GoVersion}}')
check_dir=$(mktemp -d)
trap 'rm -rf "$check_dir"' EXIT HUP INT TERM
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@"$OAPI_CODEGEN_VERSION" \
  -generate types,client -package client -o "$check_dir/client.gen.go" "$project_dir/openapi.json"
cat > "$check_dir/go.mod" <<EOF
module github.com/kilianlabs/image-transform/clientcheck

go $go_version

require (
  github.com/kilianlabs/image-transform v0.0.0
  github.com/oapi-codegen/runtime $OAPI_RUNTIME_VERSION
  github.com/getkin/kin-openapi $KIN_OPENAPI_VERSION
)

replace github.com/kilianlabs/image-transform => $project_dir
EOF
cp "$project_dir/scripts/clientcheck_test.go.txt" "$check_dir/client_test.go"
cp "$project_dir/openapi.json" "$check_dir/openapi.json"
cp -R "$project_dir/internal/native/testdata" "$check_dir/testdata"
cd "$check_dir"
go mod tidy
go test -race -v ./...
