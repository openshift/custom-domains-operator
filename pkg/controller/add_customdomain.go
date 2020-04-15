package controller

import (
	"github.com/dustman9000/custom-domain-operator/pkg/controller/customdomain"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, customdomain.Add)
}
