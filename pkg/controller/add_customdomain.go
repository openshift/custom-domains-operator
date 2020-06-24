package controller

import (
	"github.com/openshift/custom-domains-operator/pkg/controller/customdomain"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, customdomain.Add)
}
