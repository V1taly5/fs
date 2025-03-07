package discover

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"fs/internal/node"
	"fs/internal/util/logger/sl"
)

type PeerDiscoveryManager struct {
	config          *PeerDiscoveryConfig
	node            *node.Node
	log             *slog.Logger
	discoveredPeers *sync.Map
	peerConnections *sync.Map
	cancel          context.CancelFunc
}

// NewPeerDiscoveryManager creates a new discovery manager
func NewPeerDiscoveryManager(
	ctx context.Context,
	n *node.Node,
	config *PeerDiscoveryConfig,
	log *slog.Logger,
) *PeerDiscoveryManager {
	ctx, cancel := context.WithCancel(ctx)

	manager := &PeerDiscoveryManager{
		config:          config,
		node:            n,
		log:             log,
		discoveredPeers: &sync.Map{},
		peerConnections: &sync.Map{},
		cancel:          cancel,
	}

	go manager.startDiscovery(ctx)
	return manager
}

// startDiscovery initializes multiple discovery mechanisms
func (m *PeerDiscoveryManager) startDiscovery(ctx context.Context) {
	discoveries := []func(context.Context){
		m.startMulticastDiscovery,
		m.startPeriodicPeerValidation,
		m.startListening,
	}

	var wg sync.WaitGroup
	for _, discoveryFunc := range discoveries {
		wg.Add(1)
		go func(df func(context.Context)) {
			defer wg.Done()
			df(ctx)
		}(discoveryFunc)
	}

	wg.Wait()
}

// startMulticastDiscovery handles UDP multicast-based peer discovery
func (m *PeerDiscoveryManager) startMulticastDiscovery(ctx context.Context) {
	addr, err := net.ResolveUDPAddr("udp", m.config.MulticastAddress)
	if err != nil {
		m.log.Error("Failed to resolve multicast address", sl.Err(err))
		return
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		m.log.Error("Failed to create UDP connection", sl.Err(err))
		return
	}
	defer conn.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			discoveryPayload := m.generateDiscoveryPayload()
			_, err = conn.Write(discoveryPayload)
			if err != nil {
				m.log.Error("Discovery payload send failed", sl.Err(err))
			}
			time.Sleep(m.config.DiscoveryInterval)
		}
	}
}

// generateDiscoveryPayload creates a secure discovery message
func (m *PeerDiscoveryManager) generateDiscoveryPayload() []byte {
	token := make([]byte, 16)
	rand.Read(token)
	return []byte(fmt.Sprintf(
		"meow:%s:%d:%s",
		hex.EncodeToString(m.node.PubKey),
		m.node.Port,
		hex.EncodeToString(token),
	))
}

// startPeriodicPeerValidation checks and maintains peer health
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

// validateAndPrunePeers removes stale or unresponsive peers
func (m *PeerDiscoveryManager) validateAndPrunePeers() {
	m.discoveredPeers.Range(func(key, value interface{}) bool {
		peerAddr := key.(string)
		conn, err := net.DialTimeout("tcp", peerAddr, m.config.ConnectionTimeout)
		if err != nil {
			m.log.Debug("Removing unresponsive peer", slog.String("address", peerAddr))
			m.discoveredPeers.Delete(key)
			return true
		}
		conn.Close()
		return true
	})
}

// listen

func (m *PeerDiscoveryManager) startListening(ctx context.Context) {
	addr, err := net.ResolveUDPAddr("udp", m.config.MulticastAddress)
	if err != nil {
		m.log.Error("Failed to resolve multicast address", sl.Err(err))
		return
	}
	conn, err := net.ListenMulticastUDP("udp", nil, addr)
	if err != nil {
		m.log.Error("listen multicast UDP", sl.Err(err))
		return
	}
	defer conn.Close()

	// Установка размера буфера для UDP
	if err := conn.SetReadBuffer(1024 * 1024); err != nil {
		m.log.Error("set read buffer", sl.Err(err))
		return
	}

	m.log.Info("UDP Discovery listener started",
		slog.String("multicast_group", addr.String()),
	)

	buffer := make([]byte, 1024)
	for {
		select {
		case <-ctx.Done():
			m.log.Info("UDP Discovery listener stopped")
			return
		default:
			n, remoteAddr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				m.log.Error("Error reading UDP", sl.Err(err))
				continue
			}

			go m.processDiscoveryMessage(buffer[:n], remoteAddr)
		}
	}
}

func (m *PeerDiscoveryManager) processDiscoveryMessage(
	message []byte,
	remoteAddr *net.UDPAddr,
) {
	// парсинг сообщения: meow:PublicKey:Port:Token
	parts := strings.Split(string(message), ":")
	if len(parts) != 4 {
		m.log.Warn("Invalid discovery message format")
		return
	}

	publicKeyHex := parts[1]
	portStr := parts[2]

	// Декодирование публичного ключа
	publicKey, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		m.log.Error("Failed to decode public key", sl.Err(err))
		return
	}

	// Проверка, что это не наш собственный узел
	if bytes.Equal(publicKey, m.node.PubKey) {
		return
	}

	// Парсинг порта
	port, err := strconv.Atoi(portStr)
	if err != nil {
		m.log.Error("Invalid port in discovery message", sl.Err(err))
		return
	}

	// Формирование полного адреса пира
	peerAddress := fmt.Sprintf("%s:%d", remoteAddr.IP.String(), port)

	// Проверка существования пира
	_, exists := m.node.Peers.Get(string(publicKey))
	if exists {
		return
	}

	// Логирование обнаруженного пира
	m.log.Info("New peer discovered",
		slog.String("address", peerAddress),
		slog.String("public_key", publicKeyHex),
	)

	// Добавление в список обнаруженных пиров
	m.discoveredPeers.Store(peerAddress, publicKey)

	// Опциональная автоматическая попытка подключения
	// go m.attemptPeerConnection(peerAddress)
}

func (m *PeerDiscoveryManager) attemptPeerConnection(peerAddress string) {
	conn, err := net.DialTimeout("tcp", peerAddress, 5*time.Second)
	if err != nil {
		m.log.Error("Failed to connect to discovered peer",
			slog.String("address", peerAddress),
			sl.Err(err),
		)
		return
	}
	defer conn.Close()

	// Здесь можно добавить логику рукопожатия или аутентификации
	m.log.Info("Successfully connected to discovered peer",
		slog.String("address", peerAddress),
	)
}

func (m *PeerDiscoveryManager) GetDiscoveredPeers() []string {
	peers := []string{}
	m.discoveredPeers.Range(func(key, value interface{}) bool {
		peers = append(peers, key.(string))
		return true
	})
	return peers
}

// Shutdown gracefully stops all discovery mechanisms
func (m *PeerDiscoveryManager) Shutdown() {
	if m.cancel != nil {
		m.cancel()
	}
}
