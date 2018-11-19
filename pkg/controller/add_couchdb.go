package controller

import (
	"github.com/operator-framework/operator-sdk-samples/couchdb-operator/pkg/controller/couchdb"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, couchdb.Add)
}
