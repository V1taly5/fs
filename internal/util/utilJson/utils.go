package utiljson

import (
	"encoding/json"
	"log"
)

func ToJson(v interface{}) []byte {
	json, err := json.Marshal(v)
	if err != nil {
		log.Default().Print(err)
	}
	return json
}
