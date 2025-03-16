package multicastdiscovery

import (
	"context"
	discoverymodels "fs/internal/discovery_manager/models"
	"fs/internal/node"
	"log/slog"
	"net"
)

// MulticastDiscovery реализует механизм обнаружения через UDP multicast
type MulticastDiscovery struct {
	node             *node.Node
	config           *discoverymodels.PeerDiscoveryConfig
	log              *slog.Logger
	onPeerDiscovered func(address string, publicKey []byte)
	listenerConn     *net.UDPConn
	senderConn       *net.UDPConn
	cancel           context.CancelFunc
}
