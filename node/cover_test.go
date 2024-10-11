package node

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewCover(t *testing.T) {
	cmd := "TEST"
	message := []byte("Hello World")
	cover := NewCover(cmd, message)
	assert.Equal(t, []byte(cmd)[:cmdLen], cover.Cmd, "Cmd should match input truncated to cmdLen")
	assert.Nil(t, cover.Id, "Id should be nil")
	assert.Equal(t, make([]byte, fromLen), cover.From, "From should be initialized to zero bytes")
	assert.Equal(t, make([]byte, toLen), cover.To, "To should be initialized to zero bytes")
	assert.Equal(t, make([]byte, signLen), cover.Sign, "Sign should be initialized to zero bytes")
	assert.Equal(t, message, cover.Message, "Message should match input")
}

func TestNewSignedCover(t *testing.T) {
	cmd := "TEST"
	from := []byte("FROM12345678901234567890123456789012")
	to := []byte("TO1234567890123456789012345678901234")
	sign := []byte("SIGNATURE123456789012345678901234567890123456789012345678901234")
	message := []byte("Signed message")

	cover := NewSignedCover(cmd, from, to, sign, message)

	assert.Equal(t, from, cover.From, "From should match input")
	assert.Equal(t, to, cover.To, "To should match input")
	assert.Equal(t, sign, cover.Sign, "Sign should match input")
	assert.Equal(t, message, cover.Message, "Message should match input")
}

func TestCover_SerializeAndDeserialize(t *testing.T) {
	tests := []struct {
		name    string
		cover   *Cover
		isValid bool
		length  uint16
		message []byte
	}{
		{
			name: "Valid cover with message",
			cover: &Cover{
				Cmd:     []byte{0x01, 0x02, 0x03, 0x04},
				Id:      []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
				From:    []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20},
				To:      []byte{0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E, 0x2F, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x3D, 0x3E, 0x3F, 0x40},
				Sign:    []byte{0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4A, 0x4B, 0x4C, 0x4D, 0x4E, 0x4F, 0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5A, 0x5B, 0x5C, 0x5D, 0x5E, 0x5F, 0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0x6A, 0x6B, 0x6C, 0x6D, 0x6E, 0x6F, 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7A, 0x7B, 0x7C, 0x7D, 0x7E, 0x7F, 0x80},
				Length:  4,
				Message: []byte{0x0B, 0x0C, 0x0D, 0x0E},
			},
			isValid: true,
			length:  4,
			message: []byte{0x0B, 0x0C, 0x0D, 0x0E},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serialized := tt.cover.Serialize()
			fmt.Println(string(serialized))
			deserialized := Deserialize(serialized)
			fmt.Println(deserialized)

			if tt.isValid {
				assert.NotNil(t, deserialized)
				assert.Equal(t, tt.cover.Cmd, deserialized.Cmd)
				assert.Equal(t, tt.cover.Id, deserialized.Id)
				assert.Equal(t, tt.cover.From, deserialized.From)
				assert.Equal(t, tt.cover.To, deserialized.To)
				assert.Equal(t, tt.cover.Sign, deserialized.Sign)
				assert.Equal(t, tt.length, deserialized.Length)
				assert.Equal(t, tt.message, deserialized.Message)
			} else {
				assert.Nil(t, deserialized)
			}
		})
	}
}
