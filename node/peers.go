package node

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"sync"
)

type HandShake struct {
	Name     string
	PubKey   string
	EphemKey string
}

type SharedKey struct {
	LocalKey  []byte //exchLovalPrivKey
	RemoteKey []byte
	Secret    []byte
}

func (h HandShake) ToJson() []byte {
	return toJson(h)
}

func (sk *SharedKey) Update(remoteKey []byte, localKey []byte) {
	if remoteKey != nil {
		sk.RemoteKey = remoteKey
	}

	if localKey != nil {
		sk.LocalKey = localKey
	}
}

type Peer struct {
	PubKey    ed25519.PublicKey
	Conn      *net.Conn
	Name      string
	Peers     *Peers
	SharedKey SharedKey
}

type Peers struct {
	sync.RWMutex
	peers map[string]*Peer
}

func NewPeer(conn net.Conn) *Peer {
	return &Peer{
		PubKey: nil,
		Conn:   &conn,
		Name:   conn.RemoteAddr().String(),
		Peers:  NewPeers(),
		SharedKey: SharedKey{
			RemoteKey: nil,
			LocalKey:  nil,
			Secret:    nil,
		},
	}
}

func NewPeers() *Peers {
	return &Peers{
		peers: make(map[string]*Peer),
	}
}

func (p *Peer) UpdatePeer(cover *Cover) error {
	if string(cover.Cmd) != "HAND" {
		return errors.New("Invalid command")
	}
	handShake := &HandShake{}
	err := json.Unmarshal(cover.Message, handShake)
	if err != nil {
		return err
	}

	pubKey, err := hex.DecodeString(handShake.PubKey)
	if err != nil {
		return err
	}

	// TODO: проверить подпись
	isValid := ed25519.Verify(pubKey, cover.Message, cover.Sign)
	if isValid {
		ephemKey, err := hex.DecodeString(handShake.EphemKey)
		if err != nil {
			return err
		}
		p.Name = handShake.Name
		p.PubKey = pubKey

		p.SharedKey.Update(ephemKey, nil)
		return nil
	} else {
		return errors.New("Invalid Sign verification")
	}
}

func (p *Peers) Put(peer *Peer) {
	p.Lock()
	defer p.Unlock()
	p.peers[string(peer.PubKey)] = peer
}

func (p *Peers) Get(key string) (peer *Peer, found bool) {
	p.RLock()
	defer p.RUnlock()
	peer, found = p.peers[key]
	return nil, false
}

func (p *Peers) Remove(key *Peer) {
	p.RLock()
	defer p.RUnlock()
	delete(p.peers, string(key.PubKey))
}
