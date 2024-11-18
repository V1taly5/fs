package node

// import (
// 	"bytes"
// 	"crypto/ed25519"
// 	"encoding/gob"
// 	"log"

// 	"github.com/dgraph-io/badger"
// )

// type FileMetadata struct {
// 	Path       string
// 	ChunkIndex int
// 	WeakHash   uint32
// 	StrongHash []byte
// }

// func serializeFileMetadataList(files []*FileMetadata) ([]byte, error) {
// 	buffer := bytes.NewBuffer(nil)
// 	encoder := gob.NewEncoder(buffer)
// 	err := encoder.Encode(files)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return buffer.Bytes(), nil
// }

// func deserializeFileMetadataList(data []byte) ([]*FileMetadata, error) {
// 	buffer := bytes.NewBuffer(data)
// 	decoder := gob.NewDecoder(buffer)
// 	var files []*FileMetadata
// 	err := decoder.Decode(&files)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return files, nil
// }

// func serializeFileMetadata(fileMeta *FileMetadata) ([]byte, error) {
// 	buffer := bytes.NewBuffer(nil)
// 	encoder := gob.NewEncoder(buffer)
// 	err := encoder.Encode(fileMeta)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return buffer.Bytes(), nil
// }

// func deserializeFileMetadata(data []byte) (*FileMetadata, error) {
// 	buffer := bytes.NewBuffer(data)
// 	decoder := gob.NewDecoder(buffer)
// 	var fileMeta FileMetadata
// 	err := decoder.Decode(&fileMeta)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return &fileMeta, nil
// }

// func (n *Node) handleNotification(peer *Peer, cover *Cover) {
// 	changedFiles, err := deserializeFileMetadataList(cover.Message)
// 	if err != nil {
// 		log.Println("Ошибка десериализации уведомления:", err)
// 		return
// 	}

// 	// Добавляем файлы в очередь на синхронизацию или сразу начинаем синхронизацию
// 	for _, fileMeta := range changedFiles {
// 		syncFile(peer, n, fileMeta, n.db)
// 	}
// }

// func (n *Node) handleMetadataRequest(peer *Peer, cover *Cover) {
// 	// Получаем локальные метаданные
// 	metadata := getLocalMetadata(n.db)
// 	messageBytes, err := serializeFileMetadataList(metadata)
// 	if err != nil {
// 		log.Println("Ошибка сериализации метаданных:", err)
// 		return
// 	}

// 	responseCover := NewSignedCover("META_RES", n.PubKey, peer.PubKey, ed25519.Sign(n.PrivKey, messageBytes), messageBytes)
// 	// responseCover := NewCover("META", message)
// 	err = responseCover.Send(peer)
// 	if err != nil {
// 		log.Println("Ошибка отправки метаданных:", err)
// 	}
// }

// func handleMetadataResponse(peer *Peer, cover *Cover) {
// 	// Обработка полученного ответа с метаданными

// 	metadata, err := deserializeFileMetadataList(cover.Message)
// 	if err != nil {
// 		log.Println("Ошибка десериализации метаданных:", err)
// 		return
// 	}

// 	// Сравнение локальных метаданных с полученными
// 	for _, remoteMeta := range metadata {
// 		syncFile(peer, remoteMeta)
// 	}
// }

// func (n *Node) handleDataRequest(peer *Peer, cover *Cover) {
// 	// Десериализуем запрос данных
// 	var dataRequest FileMetadata
// 	err := gob.NewDecoder(bytes.NewReader(cover.Message)).Decode(&dataRequest)
// 	if err != nil {
// 		log.Println("Ошибка десериализации запроса данных:", err)
// 		return
// 	}

// 	// Получаем данные блока
// 	chunkData, err := getChunkData(dataRequest.Path, dataRequest.ChunkIndex)
// 	if err != nil {
// 		log.Println("Ошибка получения данных блока:", err)
// 		return
// 	}

// 	// Отправляем данные блока
// 	responseCover := NewSignedCover("DATA", n.PubKey, peer.PubKey, ed25519.Sign(n.PrivKey, chunkData), chunkData)
// 	err = responseCover.Send(peer)
// 	if err != nil {
// 		log.Println("Ошибка отправки данных блока:", err)
// 	}
// }

// func syncFile(peer *Peer, n Node, fileMeta *FileMetadata, db *badger.DB) {
// 	// Сравниваем локальные метаданные с полученными
// 	localMeta, err := getIndex(db, fileMeta.Path, fileMeta.ChunkIndex)
// 	if err != nil || !bytes.Equal(localMeta.StrongHash, fileMeta.StrongHash) {
// 		// Если блок отсутствует или отличается, запрашиваем данные блока

// 		requestData(peer, n, fileMeta)
// 	}
// }

// func requestData(peer *Peer, n Node, fileMeta *FileMetadata) {
// 	// Сериализуем запрос
// 	messageBytes, err := serializeFileMetadata(fileMeta)
// 	if err != nil {
// 		log.Println("Ошибка сериализации запроса данных:", err)
// 		return
// 	}
// 	responseCover := NewSignedCover("NOTF", n.PubKey, peer.PubKey, ed25519.Sign(n.PrivKey, messageBytes), messageBytes)
// 	// requestCover := NewCover("DATA", messageBytes)
// 	err = responseCover.Send(peer)
// 	if err != nil {
// 		log.Println("Ошибка отправки запроса данных:", err)
// 	}
// }
//
