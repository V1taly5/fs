package multicastdiscovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	discoverymodels "fs/internal/discovery_manager/models"
	"fs/internal/node"
	"fs/internal/util/logger/sl"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
)

// NewMulticastDiscovery создает новый механизм обнаружения через multicast
func NewMulticastDiscovery(
	node *node.Node,
	config *discoverymodels.PeerDiscoveryConfig,
	log *slog.Logger,
) *MulticastDiscovery {
	return &MulticastDiscovery{
		node:   node,
		config: config,
		log:    log.With(slog.String("discovery", "multicast")),
	}
}

// Name возвращает имя механизма
func (m *MulticastDiscovery) Name() string {
	return "multicast"
}

// SetOnPeerDiscovered устанавливает callback для обработки обнаруженных пиров
func (m *MulticastDiscovery) SetOnPeerDiscovered(callback func(address string, publicKey []byte)) {
	m.onPeerDiscovered = callback
}

// Start запускает механизм обнаружения
func (m *MulticastDiscovery) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	// Запуск отправки сообщений
	go m.startSending(ctx)

	// Запуск прослушивания
	go m.startListening(ctx)

	return nil
}

// Stop останавливает механизм обнаружения
func (m *MulticastDiscovery) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}

	if m.listenerConn != nil {
		m.listenerConn.Close()
	}

	if m.senderConn != nil {
		m.senderConn.Close()
	}

	return nil
}

// startSending запускает отправку сообщений обнаружения
func (m *MulticastDiscovery) startSending(ctx context.Context) {
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
	m.senderConn = conn

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

// generateDiscoveryPayload создает сообщение для обнаружения
func (m *MulticastDiscovery) generateDiscoveryPayload() []byte {
	token := make([]byte, 16)
	rand.Read(token)
	return []byte(fmt.Sprintf(
		"meow:%s:%d:%s",
		hex.EncodeToString(m.node.PubKey),
		m.node.Port,
		hex.EncodeToString(token),
	))
}

// startListening запускает прослушивание сообщений обнаружения
func (m *MulticastDiscovery) startListening(ctx context.Context) {
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
	m.listenerConn = conn

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

// processDiscoveryMessage обрабатывает полученное сообщение обнаружения
func (m *MulticastDiscovery) processDiscoveryMessage(
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

	// Парсинг порта
	port, err := strconv.Atoi(portStr)
	if err != nil {
		m.log.Error("Invalid port in discovery message", sl.Err(err))
		return
	}

	// Формирование полного адреса пира
	peerAddress := fmt.Sprintf("%s:%d", remoteAddr.IP.String(), port)

	// Вызов обработчика обнаружения пира
	if m.onPeerDiscovered != nil {
		m.onPeerDiscovered(peerAddress, publicKey)
	}
}
