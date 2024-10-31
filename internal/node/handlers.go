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
	n.SendName(peer)
	return
}

func (n Node) onMess(peer *Peer, cover *Cover) {
	// cover.Message = Decrypt(cover.Message, peer.SharedKey.Secret)
	n.Broker <- cover
	return
}

// func (n Node) HandleFileCover(peer *Peer, cover *Cover) {
// 	const op = "node.HandleFileCover"
// 	log := n.log.With(slog.String("op", op))

// 	// Получаем имя файла из сообщения cover
// 	fileName := string(cover.Message)
// 	log.Info("Receiving file...", slog.String("fileName", fileName), slog.Any("address", (*peer.Conn).RemoteAddr()))

// 	// Создаем файл с полученным именем для записи полученных данных
// 	file, err := os.Create(fileName)
// 	if err != nil {
// 		log.Error("Failed to create file", slog.String("fileName", fileName), sl.Err(err))
// 		return
// 	}
// 	defer file.Close()

// 	// Инициализируем буфер для хранения данных и определяем оставшийся размер данных
// 	buffer := make([]byte, 1024)
// 	remainingBytes := int(cover.Length)

// 	// Цикл для получения всех оставшихся байтов файла
// 	for remainingBytes > 0 {
// 		// Читаем данные из соединения
// 		nBytes, err := (*peer.Conn).Read(buffer)
// 		if err != nil {
// 			log.Error("Error reading file chunk", sl.Err(err))
// 			return
// 		}

// 		// Проверяем, если прочитанных байтов больше, чем оставшийся размер
// 		if nBytes > remainingBytes {
// 			nBytes = remainingBytes
// 		}

// 		// Записываем прочитанные данные в файл
// 		_, err = file.Write(buffer[:nBytes])
// 		if err != nil {
// 			log.Error("Error writing to file", sl.Err(err))
// 			return
// 		}

// 		// Уменьшаем оставшийся размер на количество записанных байтов
// 		remainingBytes -= nBytes
// 	}

// 	// Логируем успешное получение файла
// 	log.Info("File received successfully", slog.String("fileName", fileName))
// 	return
// }
