// THIS FILE IS GENERATED BY BOILERPLATE. DO NOT EDIT.
//go:build osde2e
// +build osde2e

package osde2etests

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"path/filepath"
	"testing"
)

const (
	testResultsDirectory = "/test-run-results"
	jUnitOutputFilename  = "junit-custom-domains-operator.xml"
)

// Test entrypoint. osde2e runs this as a test suite on test pod.
func TestCustomDomainsOperator(t *testing.T) {
	RegisterFailHandler(Fail)

	suiteConfig, reporterConfig := GinkgoConfiguration()
	reporterConfig.JUnitReport = filepath.Join(testResultsDirectory, jUnitOutputFilename)
	RunSpecs(t, "Custom Domains Operator", suiteConfig, reporterConfig)

}
