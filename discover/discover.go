package discover

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"fs/internal/util/logger/sl"
	"fs/node"
	"log/slog"
	"net"
	"os"
	"time"
)

var peers = make(map[string]string)

func StartDiscover(n *node.Node, peersFile string, log *slog.Logger) {
	const op = "discover.StartDiscover"
	log = log.With(
		slog.String("op", op),
	)
	go startMeow("224.0.0.1:35035", n, log)
	go listenMeow("224.0.0.1:35035", n, log, connectToPeer)

	file, err := os.Open(peersFile)
	if err != nil {
		log.Error("DISCOVER: Open peers.txt error", sl.Err(err))
		return
	}
	var lastPeers []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lastPeers = append(lastPeers, scanner.Text())
	}

	log.Info("DISCOVER: Start peer discovering. Last seen peers",
		slog.Int("len last Peers", len(lastPeers)),
	)
	for _, peerAddress := range lastPeers {
		go connectToPeer(n, peerAddress, log)
	}
}

func connectToPeer(n *node.Node, peerAddress string, log *slog.Logger) {
	const op = "discover.connectToPeer"
	log = log.With(slog.String("op", op))

	// При отключении пира записть в []peers об отключенном пире не убирается,
	// что вызывает при повторном подключении запись о том, что пир уже существует
	// Это из-за ошибки чтения cover
	if _, exist := peers[peerAddress]; exist {
		log.Debug("peer already exist", slog.String("peer address", peerAddress))
		return
	}

	peers[peerAddress] = peerAddress

	log.Debug("Try to connection peer", slog.String("peer address", peerAddress))

	conn, err := net.Dial("tcp", peerAddress)
	if err != nil {
		log.Error("Dial ERROR", sl.Err(err))
		return
	}
	defer conn.Close()

	peer := handShake(n, conn, log)

	if peer == nil {
		log.Error("Fail on handshake")
		return
	}

	n.RegisterPeer(peer)

	n.ListenPeer(peer)

	n.UnregisterPeer(peer)

	delete(peers, peerAddress)
}

// Отправка UPD multicast пакетов с информацией о себе
func startMeow(address string, n *node.Node, log *slog.Logger) {
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
		_, err = conn.Write([]byte(fmt.Sprintf("meow:%v:%v", hex.EncodeToString(n.PubKey), n.Port)))
		if err != nil {
			log.Error("conn Write Error", sl.Err(err))
		}
		time.Sleep(1 * time.Second)
	}
}

func listenMeow(address string, n *node.Node, log *slog.Logger, handler func(n *node.Node, peerAddress string, log *slog.Logger)) {
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

	err = conn.SetReadBuffer(1024)
	if err != nil {
		log.Error("SetReadBuffer Error", sl.Err(err))
	}

	for {
		buffer := make([]byte, 1024)
		_, src, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Error("ReadFromUDP failed", sl.Err(err))
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

		handler(n, peerAddress, log)
	}
}

func handShake(n *node.Node, conn net.Conn, log *slog.Logger) *node.Peer {
	log.Info("DISCOVERY: try handshake with", slog.Any("Remote addr", conn.RemoteAddr()))
	peer := node.NewPeer(conn)

	n.SendName(peer)

	cover, err := node.ReadCover(bufio.NewReader(conn))
	if err != nil {
		log.Error("Error on read Cover", sl.Err(err))
		return nil
	}
	if string(cover.Cmd) == "HAND" {
		if _, found := n.Peers.Get(string(cover.From)); found {
			log.Info(" - - - - - - - - - - - - - - - --  -- - - - - Peer (%s) already exist", slog.String("Peer name", peer.Name))
			return nil
		}
	}

	err = peer.UpdatePeer(cover)
	if err != nil {
		log.Error("HandShake error", sl.Err(err))
	}
	return peer
}
