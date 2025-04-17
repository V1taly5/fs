package discoverymanager

import (
	"context"
	discoverymodels "fs/internal/discovery_manager/models"
	"fs/internal/node"
	nodestorage "fs/internal/storage/node_storage"
	"log/slog"
	"sync"
)

// PeerDiscoveryManager управляет различными механизмами обнаружения пиров
type PeerDiscoveryManager struct {
	config          *discoverymodels.PeerDiscoveryConfig
	node            *node.Node
	node_storage    NodeDB
	log             *slog.Logger
	discoveredPeers *sync.Map
	peerConnections *sync.Map
	mechanisms      map[string]discoverymodels.DiscoveryMechanism
	mechanismsLock  sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
}

type NodeDB interface {
	SaveNode(context.Context, nodestorage.Node) error
	GetNode(context.Context, string) (nodestorage.Node, error)
	ListNodes(context.Context, nodestorage.NodeFilter) ([]nodestorage.Node, error)
	DeleteEndpoint(ctx context.Context, endpointID int64) error
	AddEndpoint(ctx context.Context, nodeID string, newEndpoint nodestorage.Endpoint) error
}
