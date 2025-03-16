package discoverymanager

import (
	"context"
	discoverymodels "fs/internal/discovery_manager/models"
	"fs/internal/node"
	"log/slog"
	"sync"
)

// PeerDiscoveryManager управляет различными механизмами обнаружения пиров
type PeerDiscoveryManager struct {
	config          *discoverymodels.PeerDiscoveryConfig
	node            *node.Node
	log             *slog.Logger
	discoveredPeers *sync.Map
	peerConnections *sync.Map
	mechanisms      map[string]discoverymodels.DiscoveryMechanism
	mechanismsLock  sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
}
