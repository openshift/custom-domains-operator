include boilerplate/generated-includes.mk

.PHONY: boilerplate-update
boilerplate-update:
	@boilerplate/update

# TODO: Temporary until prow config is fixed
.PHONY: gobuild
gobuild: go-build

# TODO: Temporary until prow config is fixed
.PHONY: gocheck
gocheck: go-check
