# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the OpenShift Dedicated Custom Domain Operator, a Kubernetes operator that creates custom ingress controllers with TLS certificates for custom domains. The operator watches for `CustomDomain` custom resources and creates corresponding `IngressController` resources in the `openshift-ingress-operator` namespace.

**Key Architecture Pattern**: CustomDomain CR → IngressController → Router pods
- Custom resources are cluster-scoped and trigger the creation of ingress controllers
- The operator creates managed ingress controllers with custom certificates
- External DNS can create CNAME records pointing to the custom domain endpoints

## Development Commands

### Building and Testing
```bash
# Build the operator binary
make go-build

# Run unit tests
make go-test

# Run unit tests with coverage for customdomain controller specifically
go test ./controller/ -coverprofile /tmp/cp.out && go tool cover -html /tmp/cp.out

# Run linting and static analysis
make go-check

# Generate CRDs and manifests
make generate

# Validate that generated files are up-to-date
make generate-check

# Run all checks (lint + test + build)
make default
```

### Container Operations
```bash
# Build container image
make docker-build

# Build and push container image (requires REGISTRY_USER and REGISTRY_TOKEN)
make docker-push

# Build and push in one step
make build-push
```

### Local Development
```bash
# Apply CRDs to cluster
oc apply -f deploy/crds/managed.openshift.io_customdomains_crd.yaml

# Run operator locally (outside cluster)
operator-sdk run --local --namespace ''

# Deploy to cluster
oc apply -f deploy/
```

## Code Architecture

### Core Components

- **`api/v1alpha1/`**: Custom Resource Definitions
  - `customdomain_types.go`: Defines the CustomDomain CRD spec and status
  - Key types: `CustomDomainSpec`, `CustomDomainStatus`, `CustomDomainCondition`

- **`controller/`**: Controller logic
  - `customdomain_controller.go`: Main reconciliation logic for CustomDomain resources
  - `customdomain_utils.go`: Utility functions for controller operations
  - Uses controller-runtime pattern with Reconcile() method

- **`main.go`**: Operator entry point
  - Sets up manager, registers schemes, and starts controller

### Key Constants and Validation
- Restricted ingress names: `["default", "apps2", "apps"]`
- Valid object names must match DNS-1035 label format: `^[a-z]([-a-z0-9]*[a-z0-9])?$`
- Default scope: "External" (can be "Internal")
- Supports AWS load balancer types: "Classic" and "NLB"

### Important Namespaces
- `openshift-ingress`: Where router pods are created
- `openshift-ingress-operator`: Where IngressController resources are managed
- Custom domain operator typically runs in `openshift-custom-domains-operator`

## Build System

This project uses a sophisticated build system based on boilerplate makefiles:

- **Main Makefile**: Includes boilerplate and delegates to generated targets
- **Boilerplate system**: Provides standardized targets via `boilerplate/generated-includes.mk`
- **FIPS support**: Enabled by default (`FIPS_ENABLED=true`)
- **Konflux builds**: Enabled for CI/CD integration

### Environment Variables
- `FIPS_ENABLED=true`: Enables FIPS mode with `GOEXPERIMENT=boringcrypto`
- `ALLOW_DIRTY_CHECKOUT=false`: Prevents building with uncommitted changes
- `GOLANGCI_LINT_CACHE=/tmp/golangci-cache`: Lint cache directory

## Testing Strategy

### Unit Testing
- Tests located in `controller/` alongside source files
- Use `make go-test` for standard testing
- Coverage reports available with `go tool cover`

### E2E Testing
- E2E tests in `test/e2e/` directory
- Integration with osde2e framework for OpenShift testing
- See `TESTING.md` for detailed testing procedures

### Live Testing Requirements
- Requires SRE setup with elevated privileges
- Need valid TLS certificates (can use Let's Encrypt for wildcards)
- External DNS configuration for CNAME records

## Deprecation Notice

On OSD/ROSA clusters > v4.14 (or v4.13 with legacy ingress support flag), this operator transitions CustomDomain objects to native OpenShift IngressController resources. The operator maintains backward compatibility during this transition.