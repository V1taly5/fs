package discoverymodels

import (
	"context"
	"time"
)

// DiscoveryMechanism представляет собой интерфейс для различных механизмов обнаружения пиров
type DiscoveryMechanism interface {
	// Start запускает механизм обнаружения
	Start(ctx context.Context) error

	// Stop останавливает механизм обнаружения
	Stop() error

	// Name возвращает имя механизма обнаружения
	Name() string

	// SetOnPeerDiscovered устанавливает callback для обработки обнаруженных пиров
	SetOnPeerDiscovered(callback func(address string, publicKey []byte))
}

// PeerDiscoveryConfig содержит конфигурацию для обнаружения пиров
type PeerDiscoveryConfig struct {
	MulticastAddress   string
	DiscoveryInterval  time.Duration
	ConnectionTimeout  time.Duration
	AutoconnectEnabled bool
}
