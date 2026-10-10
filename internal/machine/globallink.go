package machine

import (
	reconciler "github.com/rezzminator/engine-sync/internal/reconcile"
)

type GlobalLinkState = reconciler.GlobalLinkState

type GlobalLinkKind = reconciler.GlobalLinkKind

const (
	GlobalLinkMissing     = reconciler.GlobalLinkMissing
	GlobalLinkCorrect     = reconciler.GlobalLinkCorrect
	GlobalLinkCopy        = reconciler.GlobalLinkCopy
	GlobalLinkWrongTarget = reconciler.GlobalLinkWrongTarget
	GlobalLinkConflict    = reconciler.GlobalLinkConflict
	GlobalLinkDangling    = reconciler.GlobalLinkDangling
	GlobalLinkFile        = reconciler.GlobalLinkFile
	GlobalLinkDir         = reconciler.GlobalLinkDir
)

var ClassifyGlobalLink = reconciler.ClassifyGlobalLink

var ApplyGlobalLink = reconciler.ApplyGlobalLink

var DescribeGlobalLinkState = reconciler.DescribeGlobalLinkState
