# Validate variables in project.mk exist
ifndef IMAGE_REGISTRY
$(error IMAGE_REGISTRY is not set; check project.mk file)
endif
ifndef IMAGE_REPOSITORY
$(error IMAGE_REPOSITORY is not set; check project.mk file)
endif
ifndef IMAGE_NAME
$(error IMAGE_NAME is not set; check project.mk file)
endif
ifndef VERSION_MAJOR
$(error VERSION_MAJOR is not set; check project.mk file)
endif
ifndef VERSION_MINOR
$(error VERSION_MINOR is not set; check project.mk file)
endif

# Generate version and tag information from inputs
COMMIT_NUMBER=$(shell git rev-list `git rev-list --parents HEAD | egrep "^[a-f0-9]{40}$$"`..HEAD --count)
CURRENT_COMMIT=$(shell git rev-parse --short=8 HEAD)
OPERATOR_VERSION=$(VERSION_MAJOR).$(VERSION_MINOR).$(COMMIT_NUMBER)-$(CURRENT_COMMIT)

IMG?=$(IMAGE_REGISTRY)/$(IMAGE_REPOSITORY)/$(IMAGE_NAME):v$(OPERATOR_VERSION)
OPERATOR_IMAGE_URI=${IMG}
OPERATOR_IMAGE_URI_LATEST=$(IMAGE_REGISTRY)/$(IMAGE_REPOSITORY)/$(IMAGE_NAME):latest
OPERATOR_DOCKERFILE ?=build/Dockerfile

BINFILE=build/_output/bin/$(OPERATOR_NAME)
MAINPACKAGE=./cmd/manager

# Containers may default GOFLAGS=-mod=vendor which would break us since
# we're using modules.
unexport GOFLAGS
GOOS?=linux
GOARCH?=amd64
GOENV=GOOS=${GOOS} GOARCH=${GOARCH} CGO_ENABLED=0 GOFLAGS=

GOBUILDFLAGS=-gcflags="all=-trimpath=${GOPATH}" -asmflags="all=-trimpath=${GOPATH}"

TESTTARGETS := $(shell ${GOENV} go list -e ./... | egrep -v "/(vendor)/")
# ex, -v
TESTOPTS :=

ALLOW_DIRTY_CHECKOUT?=false

default: gobuild

.PHONY: clean
clean:
	rm -rf ./build/_output

.PHONY: isclean
isclean:
	@(test "$(ALLOW_DIRTY_CHECKOUT)" != "false" || test 0 -eq $$(git status --porcelain | wc -l)) || (echo "Local git checkout is not clean, commit changes and try again." >&2 && exit 1)

.PHONY: build
build: isclean envtest
	docker build . -f $(OPERATOR_DOCKERFILE) -t $(OPERATOR_IMAGE_URI)
	docker tag $(OPERATOR_IMAGE_URI) $(OPERATOR_IMAGE_URI_LATEST)

.PHONY: push
push:
	docker push $(OPERATOR_IMAGE_URI)
	docker push $(OPERATOR_IMAGE_URI_LATEST)

# These names are used by the app-sre pipeline targets
.PHONY: docker-build
docker-build: build
.PHONY: docker-push
docker-push: push

.PHONY: gocheck
gocheck: ## Lint code
	boilerplate/openshift/golang_osd_cluster_operator/golint.sh
	${GOENV} go vet ./cmd/... ./pkg/...

.PHONY: gogenerate
gogenerate:
	${GOENV} go generate $(TESTTARGETS)
	# Don't forget to commit generated files

.PHONY: opgenerate
opgenerate:
	operator-sdk generate crds
	operator-sdk generate k8s
	# Don't forget to commit generated files

.PHONY: generate
generate: opgenerate gogenerate

.PHONY: gobuild
gobuild: gocheck gotest ## Build binary
	${GOENV} go build ${GOBUILDFLAGS} -o ${BINFILE} ${MAINPACKAGE}

.PHONY: gotest
gotest:
	${GOENV} go test $(TESTOPTS) $(TESTTARGETS)

.PHONY: coverage
coverage:
	boilerplate/openshift/golang_osd_cluster_operator/codecov.sh

.PHONY: envtest
envtest: isclean
	@# test that the env target can be evaluated, required by osd-operators-registry
	@eval $$($(MAKE) env --no-print-directory) || (echo 'Unable to evaulate output of `make env`.  This breaks osd-operators-registry.' >&2 && exit 1)

.PHONY: test
test: envtest gotest

.PHONY: env
.SILENT: env
env: isclean
	echo OPERATOR_NAME=$(OPERATOR_NAME)
	echo OPERATOR_NAMESPACE=$(OPERATOR_NAMESPACE)
	echo OPERATOR_VERSION=$(OPERATOR_VERSION)
	echo OPERATOR_IMAGE_URI=$(OPERATOR_IMAGE_URI)
