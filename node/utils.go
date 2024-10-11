package node

import (
	"encoding/json"
	"log"
)

func toJson(v interface{}) []byte {
	json, err := json.Marshal(v)
	if err != nil {
		log.Default().Print(err)
	}
	return json
}
