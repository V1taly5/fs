package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"os"

	"golang.org/x/crypto/curve25519"
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

func CalcSharedSecret(publicKey []byte, privatKey []byte) (secret []byte) {
	var pubKey [32]byte
	var privKey [32]byte

	copy(pubKey[:], publicKey)
	copy(privKey[:], privatKey)
	// fmt.Println("CalcSharedSecret pubKey: ", pubKey[:])

	secret, err := curve25519.X25519(privatKey, publicKey)
	if err != nil {
		log.Default().Printf("Error: %v", err)
	}
	return
}

// Encrypt the message
func Encrypt(content []byte, key []byte) []byte {
	padding := len(content) % aes.BlockSize
	if padding != 0 {
		repeat := bytes.Repeat([]byte("\x00"), aes.BlockSize-(padding))
		content = append(content, repeat...)
	}

	if len(content)%aes.BlockSize != 0 {
		log.Printf("length must be 0 = %v", len(content)%aes.BlockSize)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}

	// The IV needs to be unique, but not secure. Therefore it's common to
	// include it at the beginning of the encrypted.
	encrypted := make([]byte, aes.BlockSize+len(content))
	iv := encrypted[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		panic(err)
	}

	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(encrypted[aes.BlockSize:], content)

	return encrypted
}

// Decrypt encrypted message
func Decrypt(encrypted []byte, key []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}

	// The IV needs to be unique, but not secure. Therefore it's common to
	// include it at the beginning of the ciphertext.
	if len(encrypted) < aes.BlockSize {
		panic("ciphertext too short")
	}
	iv := encrypted[:aes.BlockSize]
	encrypted = encrypted[aes.BlockSize:]

	// CBC mode always works in whole blocks.
	if len(encrypted)%aes.BlockSize != 0 {
		panic("ciphertext is not a multiple of the block size")
	}

	mode := cipher.NewCBCDecrypter(block, iv)

	// CryptBlocks can work in-place if the two arguments are the same.
	mode.CryptBlocks(encrypted, encrypted)

	encrypted = bytes.Trim(encrypted, string([]byte("\x00")))

	return encrypted
}
