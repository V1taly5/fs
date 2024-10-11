package node

import (
	"bufio"
	"crypto/ed25519"
	"encoding/hex"
	"io"
	"log"
	"reflect"
)

type Node struct {
	Port     int
	Name     string
	Peers    *Peers
	PubKey   ed25519.PublicKey
	PrivKey  ed25519.PrivateKey
	handlers map[string]func(peer *Peer, cover *Cover)
}

func NewNode(name string, port int) *Node {
	publicKey, privateKey := LoadKey(name)

	node := &Node{
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
	n.Peers.Remove(peer)
	log.Default().Printf("UnRegister peer: %s", peer.Name)
}

func (n Node) RegisterPeer(peer *Peer) *Peer {
	if reflect.DeepEqual(peer.PubKey, n.PubKey) {
		return nil
	}

	n.Peers.Put(peer)

	log.Default().Printf("Register now peer: %s (%v)", peer.Name, len(n.Peers.peers))

	return peer
}

func (n Node) HandleNode(rw *bufio.ReadWriter, peer *Peer) {
	for {
		cover, err := ReadCover(rw.Reader)
		if err != nil {
			if err != io.EOF {
				log.Default().Printf("Error on read Cover: %v", err)
			}
			log.Default().Printf("Disconected peer %s", peer)
			break
		}
		if ed25519.Verify(cover.From, cover.Message, cover.Sign) {
			log.Default().Printf("Signed cover!")
		}

		log.Default().Printf("LISTENER: receive cover from %s", (*peer.Conn).RemoteAddr())

		handler, found := n.handlers[string(cover.Cmd)]
		if !found {
			log.Default().Printf("LISTENER: UNHSNDLED NODE MESSAGE %v %v %v", cover.Cmd, cover.Id, cover.Message)
			continue
		}

		handler(peer, cover)
	}

	if peer != nil {
		n.UnregisterPeer(peer)
	}
}
