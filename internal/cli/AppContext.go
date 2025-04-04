package cli

import (
	"context"
	"fs/internal/discover"
	"fs/internal/node"
)

// AppContext хранит зависимости, которые будут использоваться в командах CLI
type AppContext struct {
	Node       *node.Node
	Discoverer *discover.Discoverer
	EmptyCtx   bool
	CancelFunc context.CancelFunc
	// Logger     Logger
}

func NewAppContext(node *node.Node, discover *discover.Discoverer) *AppContext {
	return &AppContext{
		Node:       node,
		Discoverer: discover,
		EmptyCtx:   false,
	}
}

func NewEmptyAppCtx(cancel context.CancelFunc) *AppContext {
	return &AppContext{
		CancelFunc: cancel,
		EmptyCtx:   true,
	}
}
