# Injected by boilerplate/openshift/golang_osd_cluster_operator/update
include boilerplate/openshift/golang_osd_cluster_operator/includes.mk

SHELL := /usr/bin/env bash
GOTEST_PACKAGES ?= ./cmd/... ./pkg/...

.PHONY: update_boilerplate
update_boilerplate:
	@boilerplate/update
