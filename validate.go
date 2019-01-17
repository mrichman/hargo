package hargo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"
)

// Validate validates the format of a .har file
func Validate(r *bufio.Reader) (bool, error) {
	dec := json.NewDecoder(r)
	var har Har
	err := dec.Decode(&har)
	if err != nil {
		log.Error(err)
		if ute, ok := err.(*json.UnmarshalTypeError); ok {
			log.Errorf("UnmarshalTypeError Value: %v - Type: %v - Offset: %v\n", ute.Value, ute.Type, ute.Offset)
		} else if se, ok := err.(*json.SyntaxError); ok {
			log.Errorf("SyntaxError %v - Offset: %v\n", err, se.Offset)
		} else {
			log.Error("Other error: ", err)
		}

		os.Exit(-2)
	} else {
		if har.Log.Version == "1.2" {
			fmt.Println("Valid HAR file! 😊")
			return true, nil
		}
	}
	return false, nil
}
