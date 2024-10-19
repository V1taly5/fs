package node

import (
	"fs/internal/util/logger/sl"
	"log/slog"
)

func (n Node) onHand(peer *Peer, cover *Cover) {
	const op = "node.onHand"
	log := n.log.With(slog.String("op", op))
	log.Debug("onHAND")

	newPeer := NewPeer(*peer.Conn)

	err := newPeer.UpdatePeer(cover)
	if err != nil {
		log.Debug("Update peer error", sl.Err(err))
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
