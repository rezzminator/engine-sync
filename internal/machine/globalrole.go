package machine

import (
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
