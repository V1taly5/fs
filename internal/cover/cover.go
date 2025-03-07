package cover

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"fs/internal/peers"
	"io"
	"log"
)

type Cover struct {
	Cmd     []byte
	Id      []byte
	From    []byte
	To      []byte
	Sign    []byte
	Length  uint16
	Message []byte
}

var cmdLen = 4
var idLen = 16
var fromLen = 32
var toLen = 32
var signLen = 64
var headerLen = cmdLen + idLen + fromLen + toLen + signLen + 2

func NewCover(cmd string, message []byte) (cover *Cover) {
	messageLength := len(message)
	if messageLength >= 65535 {
		message = message[:65535]
	}

	cover = &Cover{
		Cmd:     []byte(cmd)[:cmdLen],
		Id:      getRandomSeed(idLen)[:idLen],
		From:    make([]byte, fromLen),
		To:      make([]byte, toLen),
		Sign:    make([]byte, signLen),
		Length:  uint16(messageLength),
		Message: message[0:messageLength],
	}
	return
}

func (c *Cover) GetCmd() []byte {
	return c.Cmd
}

func (c *Cover) GetMessage() []byte {
	return c.Message
}

func NewSignedCover(cmd string, from []byte, to []byte, sign []byte, message []byte) (cover *Cover) {
	cover = NewCover(cmd, message)
	cover.From = from
	cover.To = to
	cover.Sign = sign
	return
}

func (c *Cover) Serialize() []byte {
	result := make([]byte, 0, headerLen+len(c.Message))

	result = append(result, c.Cmd...)
	result = append(result, c.Id...)
	result = append(result, c.From...)
	result = append(result, c.To...)
	result = append(result, c.Sign...)

	messageLengthBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(messageLengthBytes, c.Length)

	result = append(result, messageLengthBytes...)
	result = append(result, c.Message...)

	return result
}

func Deserialize(b []byte) (cover *Cover) {
	messageLength := binary.BigEndian.Uint16(b[headerLen-2 : headerLen])
	if messageLength > 65535 {
		return nil
	}
	cover = &Cover{
		Cmd:  b[0:cmdLen],
		Id:   b[cmdLen : cmdLen+idLen],
		From: b[cmdLen+idLen : cmdLen+idLen+fromLen],

		To:     b[cmdLen+idLen+fromLen : cmdLen+idLen+fromLen+toLen],
		Sign:   b[cmdLen+idLen+fromLen+toLen : cmdLen+idLen+fromLen+toLen+signLen],
		Length: messageLength,
	}

	if len(b) == (headerLen + int(messageLength)) {
		cover.Message = b[headerLen:]
	} else {
		cover.Message = make([]byte, messageLength)
	}
	return

}

func ReadCover(reader *bufio.Reader) (*Cover, error) {
	header := make([]byte, headerLen)

	// Ошибка года
	// _, err := reader.Read(header)
	_, err := io.ReadFull(reader, header)

	if err != nil {
		return nil, err
	}

	cover := Deserialize(header)

	// Ошибка года 2
	// _, err = reader.Read(cover.Message)
	_, err = io.ReadFull(reader, cover.Message)

	if err != nil {
		return nil, err
	}
	return cover, nil
}

func (c Cover) Send(peer *peers.Peer) error {
	log.Default().Printf("Send %s to peer %s ", c.Cmd, peer.Name)
	_, err := (*peer.Conn).Write(c.Serialize())
	if err != nil {
		// TODO return error maybe custom
		log.Default().Printf("ERROR on write message: %v", err)
		return err
	}
	return nil
}

func getRandomSeed(l int) []byte {
	seed := make([]byte, l)
	_, err := rand.Read(seed)
	if err != nil {
		// TODO return error
		log.Printf("rand.Read Error: %v", err)
	}
	return seed
}
