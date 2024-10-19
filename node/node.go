package node

import (
	"bufio"
	"crypto/ed25519"
	"encoding/hex"
	"fs/internal/util/logger/sl"
	"io"
	"log/slog"
	"reflect"
)

type Node struct {
	log      *slog.Logger
	Port     int
	Name     string
	Peers    *Peers
	PubKey   ed25519.PublicKey
	PrivKey  ed25519.PrivateKey
	handlers map[string]func(peer *Peer, cover *Cover)
}

func NewNode(name string, port int, log *slog.Logger) *Node {
	publicKey, privateKey := LoadKey(name)

	node := &Node{
		log:      log,
		Port:     port,
		Name:     name,
		Peers:    NewPeers(),
		PubKey:   publicKey,
		PrivKey:  privateKey,
		handlers: make(map[string]func(peer *Peer, cover *Cover)),
	}
	return node
}

// rename
func (n *Node) SendName(peer *Peer) {
	ephemPubKey, epemPrivKey := CreatePairEphemeralKey()

	handShake := HandShake{
		Name:     n.Name,
		PubKey:   hex.EncodeToString(n.PubKey),
		EphemKey: hex.EncodeToString(ephemPubKey.Bytes()),
	}.ToJson()

	peer.SharedKey.Update(nil, epemPrivKey.Bytes())

	sign := ed25519.Sign(n.PrivKey, handShake)

	cover := NewSignedCover("HAND", n.PubKey, make([]byte, 32), sign, handShake)
	cover.Send(peer)
}

func (n *Node) UnregisterPeer(peer *Peer) {
	const op = "node.UnregisterPeer"
	log := n.log.With(slog.String("op", op))

	n.Peers.Remove(peer)
	log.Info("UnRegister peer", slog.String("peer name", peer.Name))
}

func (n Node) RegisterPeer(peer *Peer) *Peer {
	const op = "node.RegisterPeer"
	log := n.log.With(slog.String("op", op))

	if reflect.DeepEqual(peer.PubKey, n.PubKey) {
		return nil
	}

	n.Peers.Put(peer)

	log.Info("Register new peer", slog.Int(peer.Name, len(n.Peers.peers)))

	return peer
}

// ListenPeer Начало прослушивания соединения с пиром
func (n Node) ListenPeer(peer *Peer) {
	readWriter := bufio.NewReadWriter(bufio.NewReader(*peer.Conn), bufio.NewWriter(*peer.Conn))
	n.HandleNode(readWriter, peer)
}

func (n Node) HandleNode(rw *bufio.ReadWriter, peer *Peer) {
	const op = "node.HandleNode"
	log := n.log.With(slog.String("op", op))
	
	for {
		cover, err := ReadCover(rw.Reader)
		if err != nil {
			if err != io.EOF {
				log.Error("Error on read Cover", sl.Err(err))
			}
			log.Error("Disconected peer", sl.Err(err))
			break
		}
		if ed25519.Verify(cover.From, cover.Message, cover.Sign) {
			log.Info("Signed cover!")
		}

		log.Info("LISTENER: receive cover from",
			slog.Any("address", (*peer.Conn).RemoteAddr()),
		)

		handler, found := n.handlers[string(cover.Cmd)]
		if !found {
			log.Info("LISTENER: UNHSNDLED NODE MESSAGE",
				slog.String("CMD", string(cover.Cmd)),
				slog.String("ID", string(cover.Id)),
				slog.String("Message", string(cover.Message)),
			)
			continue
		}
		handler(peer, cover)
	}

	if peer != nil {
		n.UnregisterPeer(peer)
	}
}
