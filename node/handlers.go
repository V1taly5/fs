package node

import "log"

func (n Node) onHand(peer *Peer, cover *Cover) {
	log.Default().Print("onHAND")

	newPeer := NewPeer(*peer.Conn)

	err := newPeer.UpdatePeer(cover)
	if err != nil {
		log.Default().Printf("Update peer error: %s", err)
	} else {
		if peer != nil {
			n.UnregisterPeer(peer)
		}

		peer.Name = newPeer.Name
		peer.PubKey = newPeer.PubKey
		peer.SharedKey = newPeer.SharedKey

		n.RegisterPeer(peer)
	}
	n.RegisterPeer(peer)
	return
}
