package hargo

import (
	"bufio"
	"encoding/json"

	log "github.com/sirupsen/logrus"
)

//https://golang.org/pkg/encoding/json/#example_Decoder_Decode_stream

func readHARStream(r *bufio.Reader, entries chan Entry, stop chan bool) {

	log.Infoln("reading HAR file")
	decoder := json.NewDecoder(r)

	// navigate to entries
loop:
	for {
		t, _ := decoder.Token()
		if t == nil {
			break
		}
		switch token := t.(type) {
		case json.Token:
			if token == "entries" {
				break loop
			}
		}
	}

	// skip open bracket
	_, err := decoder.Token()
	if err != nil {
		log.Fatal(err)
	}

	// read entries
	for decoder.More() {
		var e Entry
		err := decoder.Decode(&e)
		if err != nil {
			log.Fatal(err)
		}
		if len(e.Request.URL) > 0 {
			entries <- e
		}

		select {
		default:
			continue
		case <-stop:
			log.Infoln("stop reading HAR file")
			close(entries)
			return
		}
	}
	log.Infoln("read HAR file")
	return
}
