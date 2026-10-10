package codex

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	reconciler "github.com/rezzminator/engine-sync/internal/reconcile"
)

type GlobalRoleState = reconciler.GlobalRoleState

const (
	GlobalRoleMissing   = reconciler.GlobalRoleMissing
	GlobalRoleCurrent   = reconciler.GlobalRoleCurrent
	GlobalRoleStale     = reconciler.GlobalRoleStale
	GlobalRoleOwnedLink = reconciler.GlobalRoleOwnedLink
	GlobalRoleForeign   = reconciler.GlobalRoleForeign
)

var GeneratedGlobalRole = reconciler.GeneratedGlobalRole

var ErrGlobalRoleNotRegular = reconciler.ErrGlobalRoleNotRegular

var ReadGlobalRoleFile = reconciler.ReadGlobalRoleFile

var ClassifyGlobalRole = reconciler.ClassifyGlobalRole

var ApplyGlobalRole = reconciler.ApplyGlobalRole

func globalRoleHeader(source string) string { return artifact.Marker(artifact.TOML, source) + "\n" }
