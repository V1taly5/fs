package node

import (
	"crypto/ed25519"
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

func (p *Peers) Remove(key string) {
	p.RLock()
	defer p.RUnlock()
	delete(p.peers, key)
}
