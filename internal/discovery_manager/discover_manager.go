package discoverymanager

import (
	"bytes"
	"context"
	"encoding/hex"
	multicastdiscovery "fs/internal/discovery_manager/discovery_mechanism/multicast_discovery"
	discoverymodels "fs/internal/discovery_manager/models"
	"fs/internal/node"
	"fs/internal/util/logger/sl"
	"log/slog"
	"net"
	"sync"
	"time"
)

// NewPeerDiscoveryManager создает новый менеджер обнаружения пиров
func NewPeerDiscoveryManager(
	ctx context.Context,
	n *node.Node,
	config *discoverymodels.PeerDiscoveryConfig,
	log *slog.Logger,
) *PeerDiscoveryManager {
	ctx, cancel := context.WithCancel(ctx)

	manager := &PeerDiscoveryManager{
		config:          config,
		node:            n,
		log:             log,
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
	go manager.startPeriodicPeerValidation(ctx)

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
	op := "discover_ manager.onPeerDiscovered"
	log := m.log.With(slog.String("op", op))

	// проверка, что это не наш собственный узел
	if bytes.Equal(publicKey, m.node.PubKey) {
		return
	}

	// проверка существования пира
	_, exists := m.node.Peers.Get(string(publicKey))
	if exists {
		return
	}

	// логирование обнаруженного пира
	log.Info("New peer discovered",
		slog.String("address", address),
		slog.String("public_key", hex.EncodeToString(publicKey)),
	)

	// добавление в список обнаруженных пиров
	m.discoveredPeers.Store(address, publicKey)

	// опциональная автоматическая попытка подключения
	if m.config.AutoconnectEnabled {
		go m.attemptPeerConnection(address)
	}
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

// validateAndPrunePeers удаляет неактивные пиры
func (m *PeerDiscoveryManager) validateAndPrunePeers() {
	op := "discover_ manager.validateAndPrunePeers"
	log := m.log.With(slog.String("op", op))

	m.discoveredPeers.Range(func(key, value interface{}) bool {
		peerAddr := key.(string)
		conn, err := net.DialTimeout("tcp", peerAddr, m.config.ConnectionTimeout)
		if err != nil {
			log.Debug("Removing unresponsive peer", slog.String("address", peerAddr))
			m.discoveredPeers.Delete(key)
			return true
		}
		conn.Close()
		return true
	})
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

// GetDiscoveredPeers возвращает список обнаруженных пиров
func (m *PeerDiscoveryManager) GetDiscoveredPeers() []string {
	peers := []string{}
	m.discoveredPeers.Range(func(key, value interface{}) bool {
		peers = append(peers, key.(string))
		return true
	})
	return peers
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
