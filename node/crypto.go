package node

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
)

func SaveKey(fileName string) *os.File {
	file, err := os.Create(fileName)
	if err != nil {
		// log.Error
	}
	seed := make([]byte, 64)

	_, err = rand.Reader.Read(seed)
	if err != nil {
		// log.Error
	}

	_, err = file.Write(seed)
	if err != nil {
		// log.Error
	}

	_, err = file.Seek(0, 0)
	if err != nil {
		// log.Error
	}

	return file
}

func LoadKey(name string) (pubKey ed25519.PublicKey, privKey ed25519.PrivateKey) {
	fileName := name + ".seed"
	file, err := os.Open(fileName)
	if err != nil {
		if os.IsNotExist(err) {
			file = SaveKey(fileName)
		} else if os.IsPermission(err) {
			panic(err)
		}
	}

	pubKey, privKey, err = ed25519.GenerateKey(file)
	if err != nil {
		// log.Error
	}

	return pubKey, privKey

}

func CreatePairEphemeralKey() (publicKey *ecdh.PublicKey, privatKey *ecdh.PrivateKey) {
	privatKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		log.Default().Print("Error")
	}

	publicKey = privatKey.PublicKey()
	log.Printf("Key ephemeral pair %s %s", hex.EncodeToString(publicKey.Bytes()), hex.EncodeToString(privatKey.Bytes()))

	return
}
