package node

import (
	"crypto/ed25519"
	"encoding/hex"

	"github.com/pion/dtls/v2/pkg/protocol/handshake"
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

//rename 
func (n *Node) SendName(peer *Peer) {
	ephemPubKey, epemPrivKey := CreatePairEphemeralKey()

	handShake := HandShake{
		Name: n.Name,
		PubKey: hex.EncodeToString(n.PubKey),
		EphemKey: hex.EncodeToString(ephemPubKey.Bytes()) ,
	}.ToJson()
// Implement ToJson	
	
	peer.SharedKey.Update(nil, epemPrivKey.Bytes())

	sign := ed25519.Sign(n.PrivKey, handShake)

	//TODO Implement cover and send 
}


