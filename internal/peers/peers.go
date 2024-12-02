package peers

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fs/internal/crypto"
	utiljson "fs/internal/util/utilJSON"
	"net"
	"sync"
)

type PeerManager interface {
	AddPeer(peer *Peer)
	RemovePeer(peer *Peer)
	GetPeer(peerID string) (*Peer, bool)
	ListPeers() map[string]*Peer
}

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
	return utiljson.ToJson(h)
}

func (sk *SharedKey) Update(remoteKey []byte, localKey []byte) {
	if remoteKey != nil {
		sk.RemoteKey = remoteKey
	}

	if localKey != nil {
		sk.LocalKey = localKey
	}

	if sk.LocalKey != nil && sk.RemoteKey != nil {
		secret := crypto.CalcSharedSecret(sk.RemoteKey, sk.LocalKey)
		sk.Secret = secret
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

func (p *Peers) GetPeers() map[string]*Peer {
	return p.peers
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

type CoverInterface interface {
	GetCmd() string
	GetId() []byte
	GetFrom() []byte
	GetTo() []byte
	GetSign() []byte
	GetLength() uint16
	GetMessage() []byte
}

func NewPeers() *Peers {
	return &Peers{
		peers: make(map[string]*Peer),
	}
}

// GetPubKey возвращает публичный ключ.
func (p *Peer) GetPubKey() ed25519.PublicKey {
	return p.PubKey
}

// GetConn возвращает соединение.
func (p *Peer) GetConn() *net.Conn {
	return p.Conn
}

// GetName возвращает имя пира.
func (p *Peer) GetName() string {
	return p.Name
}

// GetSharedKey возвращает объект SharedKey.
func (p *Peer) GetSharedKey() SharedKey {
	return p.SharedKey
}

// GetSharedSecret возвращает общий секретный ключ.
func (p *Peer) GetSharedSecret() []byte {
	return p.SharedKey.Secret
}

func (p *Peer) UpdatePeer(cover CoverInterface) error {
	if string(cover.GetCmd()) != "HAND" {
		return errors.New("Invalid command")
	}
	handShake := &HandShake{}
	err := json.Unmarshal(cover.GetMessage(), handShake)
	if err != nil {
		return err
	}

	pubKey, err := hex.DecodeString(handShake.PubKey)
	if err != nil {
		return err
	}

	// TODO: проверить подпись
	// isValid := ed25519.Verify(pubKey, cover.Message, cover.Sign)
	// if isValid {
	ephemKey, err := hex.DecodeString(handShake.EphemKey)
	if err != nil {
		return err
	}
	p.Name = handShake.Name
	p.PubKey = pubKey
	p.SharedKey.Update(ephemKey, nil)
	return nil
	// } else {
	// 	return errors.New("Invalid Sign verification")
	// }
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
	return
}

func (p *Peers) Gets() map[string]*Peer {
	return p.peers
}

func (p *Peers) Remove(key *Peer) {
	p.RLock()
	defer p.RUnlock()
	delete(p.peers, string(key.PubKey))
}
