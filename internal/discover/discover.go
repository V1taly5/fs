package discover

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"fs/internal/cover"
	"fs/internal/node"
	peerss "fs/internal/peers"
	"fs/internal/util/logger/sl"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"
)

type Discoverer struct {
	discoveredPeers map[string]string
	mu              sync.Mutex
	log             *slog.Logger
}

func (d *Discoverer) AddPeer(peerAddress string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.discoveredPeers[peerAddress]; !exists {
		d.discoveredPeers[peerAddress] = peerAddress
	}
}

func (d *Discoverer) GetDiscoveredPeers() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	peers := make([]string, 0, len(d.discoveredPeers))
	for _, peer := range d.discoveredPeers {
		peers = append(peers, peer)
	}
	return peers
}

func (d *Discoverer) ConnectToPeer(ctx context.Context, n *node.Node, peerAddress string) {
	const op = "discover.ConnectToPeer"
	log := d.log.With(slog.String("op", op))

	d.mu.Lock()
	if _, exists := d.discoveredPeers[peerAddress]; !exists {
		log.Debug("Peer not in discovered list", slog.String("peer address", peerAddress))
		d.mu.Unlock()
		return
	}
	// Опционально: можно удалить пира из списка после попытки подключения
	// delete(d.discoveredPeers, peerAddress)
	d.mu.Unlock()

	// Остальной код из функции connectToPeer
	conn, err := net.Dial("tcp", peerAddress)
	if err != nil {
		log.Error("Dial ERROR", sl.Err(err))
		return
	}
	defer conn.Close()

	peer := handShake(n, conn, log)
	if peer == nil {
		log.Error("Handshake failed")
		return
	}

	n.RegisterPeer(peer)
	n.ListenPeer(ctx, peer)
	n.UnregisterPeer(peer)
}

var multicastAddress string = "224.0.0.251:35035"

var peers = make(map[string]string)

func StartDiscover(ctx context.Context, n *node.Node, peersFile string, log *slog.Logger) *Discoverer {
	const op = "discover.StartDiscover"
	log = log.With(
		slog.String("op", op),
	)

	discoverer := &Discoverer{
		discoveredPeers: make(map[string]string),
		log:             log,
	}
	go startMeow(ctx, multicastAddress, n, log)
	go listenMeow(ctx, multicastAddress, n, log, discoverer)

	file, err := os.Open(peersFile)
	if err != nil {
		log.Error("DISCOVER: Open peers.txt error", sl.Err(err))
		return nil
	}
	defer file.Close()

	var lastPeers []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lastPeers = append(lastPeers, scanner.Text())
	}

	log.Info("DISCOVER: Start peer discovering. Last seen peers",
		slog.Int("len last Peers", len(lastPeers)),
	)

	return discoverer
	// for _, peerAddress := range lastPeers {
	// 	go connectToPeer(ctx, n, peerAddress, log)
	// }
}

func connectToPeer(ctx context.Context, n *node.Node, peerAddress string, log *slog.Logger) {
	const op = "discover.connectToPeer"
	log = log.With(slog.String("op", op))

	// При отключении пира записть в []peers об отключенном пире не убирается,
	// что вызывает при повторном подключении запись о том, что пир уже существует
	// Это из-за ошибки чтения cover
	if _, exist := peers[peerAddress]; exist {
		log.Debug("Peer already exists", slog.String("peer address", peerAddress))
		return
	}

	peers[peerAddress] = peerAddress

	log.Debug("Attempting to connect to peer", slog.String("peer address", peerAddress))

	conn, err := net.Dial("tcp", peerAddress)
	if err != nil {
		log.Error("Dial ERROR", sl.Err(err))
		return
	}
	defer conn.Close()

	peer := handShake(n, conn, log)
	if peer == nil {
		log.Error("Handshake failed")
		return
	}

	n.RegisterPeer(peer)
	n.ListenPeer(ctx, peer)
	n.UnregisterPeer(peer)
	delete(peers, peerAddress)
}

// Отправка UPD multicast пакетов с информацией о себе
func startMeow(ctx context.Context, address string, n *node.Node, log *slog.Logger) {
	const op = "discover.startMeow"
	log = log.With(
		slog.String("op", op),
	)

	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		log.Error("ResolveUDPAddr Error", sl.Err(err))
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Error("DialUDP Error", sl.Err(err))
	}
	for {
		select {
		case <-ctx.Done():
			log.Info("Context canceled, stopping startMeow")
			return
		default:
			_, err = conn.Write([]byte(fmt.Sprintf("meow:%v:%v", hex.EncodeToString(n.PubKey), n.Port)))
			if err != nil {
				log.Error("conn Write Error", sl.Err(err))
			}
			time.Sleep(1 * time.Second)
		}
	}
}

func listenMeow(ctx context.Context, address string, n *node.Node, log *slog.Logger, discoverer *Discoverer) {
	const op = "discover.listenMeow"
	log = log.With(slog.String("op", op))

	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		log.Error("ResolveUDPAddr Error", sl.Err(err))
	}

	conn, err := net.ListenMulticastUDP("udp", nil, addr)
	if err != nil {
		log.Error("ListenMulticastUDP Error", sl.Err(err))
	}
	defer conn.Close()

	err = conn.SetReadBuffer(1024)
	if err != nil {
		log.Error("SetReadBuffer Error", sl.Err(err))
	}

	for {
		select {
		case <-ctx.Done():
			log.Info("Context canceled, stopping listenMeow")
			return
		default:
			buffer := make([]byte, 1024)
			_, src, err := conn.ReadFromUDP(buffer)
			if err != nil {
				log.Error("ReadFromUDP failed", sl.Err(err))
				continue
			}

			trim := bytes.Trim(buffer, "\x00")
			peerPubKeySrt := string(trim[5 : 5+64])
			peerPubKey, err := hex.DecodeString(peerPubKeySrt)
			if err != nil {
				log.Error("DecodeHexString failed", sl.Err(err))
				continue
			}

			_, found := n.Peers.Get(string(peerPubKey))
			if found || bytes.Equal(n.PubKey, peerPubKey) {
				continue
			}

			peerAddress := src.IP.String() + string(trim[5+64:])
			discoverer.AddPeer(peerAddress)
		}
	}
}

func handShake(n *node.Node, conn net.Conn, log *slog.Logger) *peerss.Peer {
	log.Info("DISCOVERY: attempting handshake with", slog.Any("Remote addr", conn.RemoteAddr()))
	peer := peerss.NewPeer(conn)

	n.SendName(peer)

	cover, err := cover.ReadCover(bufio.NewReader(conn))
	if err != nil {
		log.Error("Error on read Cover", sl.Err(err))
		return nil
	}
	if string(cover.Cmd) == "HAND" {
		if _, found := n.Peers.Get(string(cover.From)); found {
			log.Info("Peer already exists", slog.String("Peer name", peer.Name))
			return nil
		}
	}

	err = peer.UpdatePeer(cover)
	if err != nil {
		log.Error("HandShake error", sl.Err(err))
		return nil
	}
	return peer
}
