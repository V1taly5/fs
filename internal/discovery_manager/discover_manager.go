package discoverymanager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	multicastdiscovery "fs/internal/discovery_manager/discovery_mechanism/multicast_discovery"
	discoverymodels "fs/internal/discovery_manager/models"
	"fs/internal/node"
	nodestorage "fs/internal/storage/node_storage"
	"fs/internal/util/logger/sl"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"
)

// NewPeerDiscoveryManager создает новый менеджер обнаружения пиров
func NewPeerDiscoveryManager(
	ctx context.Context,
	n *node.Node,
	config *discoverymodels.PeerDiscoveryConfig,
	nodeStorage NodeDB,
	log *slog.Logger,
) *PeerDiscoveryManager {
	ctx, cancel := context.WithCancel(ctx)

	manager := &PeerDiscoveryManager{
		config:          config,
		node:            n,
		log:             log,
		node_storage:    nodeStorage,
		discoveredPeers: &sync.Map{},
		peerConnections: &sync.Map{},
		mechanisms:      make(map[string]discoverymodels.DiscoveryMechanism),
		ctx:             ctx,
		cancel:          cancel,
	}

	// создаем и добавляем механизм обнаружения через multicast
	multicastMechanism := multicastdiscovery.NewMulticastDiscovery(n, config, log)
	multicastMechanism.SetOnPeerDiscovered(manager.onPeerDiscovered)
	manager.RegisterDiscoveryMechanism(multicastMechanism)

	// запускаем периодическую валидацию пиров
	// go manager.startPeriodicPeerValidation(ctx)

	return manager
}

// Start запускает все зарегистрированные механизмы обнаружения
func (m *PeerDiscoveryManager) Start() {
	op := "discover_ manager.Start"
	log := m.log.With(slog.String("op", op))
	m.mechanismsLock.RLock()
	defer m.mechanismsLock.RUnlock()

	for name, mechanism := range m.mechanisms {
		err := mechanism.Start(m.ctx)
		if err != nil {
			log.Error("Failed to start discovery mechanism",
				slog.String("mechanism", name),
				sl.Err(err))
		} else {
			log.Info("Started discovery mechanism",
				slog.String("mechanism", name))
		}
	}
}

// RegisterDiscoveryMechanism регистрирует новый механизм обнаружения
func (m *PeerDiscoveryManager) RegisterDiscoveryMechanism(mechanism discoverymodels.DiscoveryMechanism) {
	op := "discover_ manager.RegisterDiscoveryMechanism"
	log := m.log.With(slog.String("op", op))
	m.mechanismsLock.Lock()
	defer m.mechanismsLock.Unlock()

	name := mechanism.Name()
	m.mechanisms[name] = mechanism
	log.Info("Registered discovery mechanism", slog.String("mechanism", name))
}

// UnregisterDiscoveryMechanism удаляет механизм обнаружения
func (m *PeerDiscoveryManager) UnregisterDiscoveryMechanism(name string) {
	op := "discover_ manager.UnregisterDiscoveryMechanism"
	log := m.log.With(slog.String("op", op))
	m.mechanismsLock.Lock()
	defer m.mechanismsLock.Unlock()

	if mechanism, exists := m.mechanisms[name]; exists {
		mechanism.Stop()
		delete(m.mechanisms, name)
		log.Info("Unregistered discovery mechanism", slog.String("mechanism", name))
	}
}

// onPeerDiscovered обрабатывает обнаружение нового пира
func (m *PeerDiscoveryManager) onPeerDiscovered(address string, publicKey []byte) {
	op := "discover_manager.onPeerDiscovered"
	log := m.log.With(slog.String("op", op))

	// проверка, что это не наш собственный узел
	if bytes.Equal(publicKey, m.node.PubKey) {
		return
	}

	hashPublicKey := sha256.Sum256(publicKey)
	nodeID := hex.EncodeToString(hashPublicKey[:])

	_, portStr, err := net.SplitHostPort(address)
	if err != nil {
		log.Error("Failed to split host and port", sl.Err(err))
		return
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Error("Failed to convert port string to integer", sl.Err(err))
		return
	}

	endpoint := nodestorage.Endpoint{
		NodeID:   nodeID,
		Address:  address,
		Port:     port,
		Protocol: nodestorage.ProtocolTCP,
		Source:   nodestorage.SourceLocal,
	}

	node, err := m.node_storage.GetNode(m.ctx, nodeID)
	if err == nodestorage.ErrNodeNotFound {
		log.Debug("Node not fount, creating new one", sl.Err(err))
		log.Debug("New peer discovered",
			slog.String("address", address),
			slog.String("public_key", hex.EncodeToString(publicKey)),
		)

		newNode := nodestorage.Node{
			ID:        nodeID,
			PublicKey: publicKey,
			Name:      string(publicKey),
			FirstSeen: time.Now(),
			LastSeen:  time.Now(),
			Endpoints: []nodestorage.Endpoint{endpoint},
		}

		if err := m.node_storage.SaveNode(m.ctx, newNode); err != nil {
			log.Error("Failed to save new node", sl.Err(err))
			return
		}

		// TODO: plug
		if m.config.AutoconnectEnabled {
			go m.attemptPeerConnection(address)
		}
		return

	} else if err != nil {
		log.Error("Error getting node", sl.Err(err))
		return
	}

	for _, endpoint := range node.Endpoints {
		if endpoint.Address == address {
			log.Debug("Endpoint already exists", slog.String("address", address))
			return
		}
	}

	if err := m.node_storage.AddEndpoint(m.ctx, node.ID, endpoint); err != nil {
		log.Error("Failed to add new endpoint", sl.Err(err))
		return
	}
	log.Debug("Added new endpoint to existing node",
		slog.String("node_id", nodeID),
		slog.String("address", address))

	// добавление в список обнаруженных пиров
	// m.discoveredPeers.Store(address, publicKey)

}

// startPeriodicPeerValidation запускает периодическую проверку пиров
func (m *PeerDiscoveryManager) startPeriodicPeerValidation(ctx context.Context) {
	ticker := time.NewTicker(m.config.DiscoveryInterval * 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.validateAndPrunePeers()
		}
	}
}

// TODO: modification, removal of inactive peers from the map for deletion from the database - done
// validateAndPrunePeers удаляет неактивные пиры
func (m *PeerDiscoveryManager) validateAndPrunePeers() {
	op := "discover_ manager.validateAndPrunePeers"
	log := m.log.With(slog.String("op", op))

	// TODO: create filter
	nodes, err := m.node_storage.ListNodes(m.ctx, nodestorage.NodeFilter{})
	if err != nil {
		log.Error("Failed to get list nodes", sl.Err(err))
		return
	}

	for _, node := range nodes {
		for _, endpoint := range node.Endpoints {
			valid := false

			switch endpoint.Protocol {
			case nodestorage.ProtocolTCP:
				if endpoint.Source == nodestorage.SourceLocal {
					conn, err := net.DialTimeout("tcp", endpoint.Address, m.config.ConnectionTimeout)
					if err == nil {
						conn.Close()
						valid = true
					}
				}
			default:
				// skip other protocols for now
				log.Debug("Skipping validation for unsupported protocol",
					slog.String("protocol", string(endpoint.Protocol)),
					slog.String("nodeID", node.ID))
				continue

			}
			if !valid {
				log.Debug("Removing unresponsive endpoint",
					slog.String("nodeID", node.ID),
					slog.String("address", endpoint.Address),
					slog.Int("port", endpoint.Port),
					slog.String("protocol", string(endpoint.Protocol)))
				err := m.node_storage.DeleteEndpoint(m.ctx, endpoint.ID)
				if err != nil {
					log.Error("Failed to delete endpoint", slog.Int64("EndpointID", endpoint.ID), sl.Err(err))
				}
			}
		}
	}
	// for sync.Map storage
	// m.discoveredPeers.Range(func(key, value interface{}) bool {
	// 	peerAddr := key.(string)
	// 	conn, err := net.DialTimeout("tcp", peerAddr, m.config.ConnectionTimeout)
	// 	if err != nil {
	// 		log.Debug("Removing unresponsive peer", slog.String("address", peerAddr))
	// 		m.discoveredPeers.Delete(key)
	// 		return true
	// 	}
	// 	conn.Close()
	// 	return true
	// })
}

// attemptPeerConnection пытается установить соединение с пиром
func (m *PeerDiscoveryManager) attemptPeerConnection(peerAddress string) {
	op := "discover_ manager.attemptPeerConnection"
	log := m.log.With(slog.String("op", op))

	conn, err := net.DialTimeout("tcp", peerAddress, m.config.ConnectionTimeout)
	if err != nil {
		log.Error("Failed to connect to discovered peer",
			slog.String("address", peerAddress),
			sl.Err(err),
		)
		return
	}
	defer conn.Close()

	// здесь можно добавить логику рукопожатия или аутентификации
	log.Info("Successfully connected to discovered peer",
		slog.String("address", peerAddress),
	)
}

// TODO: modification, returning the list of detected peers from the database - done (GetDBDiscoveredPeers)
// GetDiscoveredPeers возвращает список обнаруженных пиров
func (m *PeerDiscoveryManager) GetDiscoveredPeers() []string {
	peers := []string{}
	m.discoveredPeers.Range(func(key, value interface{}) bool {
		peers = append(peers, key.(string))
		return true
	})
	return peers
}

func (m *PeerDiscoveryManager) GetDBDiscoveredPeers() ([]nodestorage.Node, error) {
	nodes, err := m.node_storage.ListNodes(m.ctx, nodestorage.NodeFilter{})
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

// Shutdown останавливает все механизмы обнаружения
func (m *PeerDiscoveryManager) Shutdown() {
	op := "discover_manager.Shutdown"
	log := m.log.With(slog.String("op", op))

	m.mechanismsLock.Lock()
	defer m.mechanismsLock.Unlock()

	for name, mechanism := range m.mechanisms {
		err := mechanism.Stop()
		if err != nil {
			log.Error("Error stopping discovery mechanism",
				slog.String("mechanism", name),
				sl.Err(err))
		}
	}

	if m.cancel != nil {
		m.cancel()
	}
}
