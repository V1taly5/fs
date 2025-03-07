package cli

import (
	"fs/internal/discover"
	"fs/internal/node"
)

// AppContext хранит зависимости, которые будут использоваться в командах CLI
type AppContext struct {
	Node       *node.Node
	Discoverer *discover.Discoverer
	// Logger     Logger
}

func NewAppContext(node *node.Node, discover *discover.Discoverer) *AppContext {
	return &AppContext{
		Node:       node,
		Discoverer: discover,
	}
}
