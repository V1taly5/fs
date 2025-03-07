package db

import (
	"bytes"
	"encoding/gob"
)

// Serializer предоставляет интерфейс для сериализации/десериализации данных
type Serializer interface {
	Serialize(v interface{}) ([]byte, error)
	Deserialize(data []byte, v interface{}) error
}

// GobSerializer реализует Serializer используя encoding/gob
type GobSerializer struct{}

func (s *GobSerializer) Serialize(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *GobSerializer) Deserialize(data []byte, v interface{}) error {
	return gob.NewDecoder(bytes.NewReader(data)).Decode(v)
}
