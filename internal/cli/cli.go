// Package cli adapts process arguments and IO to the compiler services.
package cli

import "github.com/rezzminator/engine-sync/internal/cliargs"

var NewFlagSet = cliargs.NewFlagSet
var ParseFlags = cliargs.ParseFlags
var ParseFlagsAnywhere = cliargs.ParseFlagsAnywhere
