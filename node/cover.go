package node

import (
	"encoding/binary"
	"fmt"
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
		Id:      nil,
		From:    make([]byte, fromLen),
		To:      make([]byte, toLen),
		Sign:    make([]byte, signLen),
		Message: message[0:messageLength],
	}
	return
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

func Dederialize(b []byte) (cover *Cover) {
	messageLength := binary.BigEndian.Uint16(b[headerLen-2 : headerLen])
	if messageLength > 65535 {
		return nil
	}
	fmt.Println(b)
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
