package hargo

import (
	"bufio"
	"encoding/json"
	"sync"

	log "github.com/sirupsen/logrus"
)

//https://golang.org/pkg/encoding/json/#example_Decoder_Decode_stream

func readHARStream(r *bufio.Reader, entries chan Entry, wg *sync.WaitGroup, stop chan bool) {
	defer wg.Done()

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
read:
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
		case <-stop: // triggered when the stop channel is closed
			log.Infoln("stop reading HAR file")
			break read // exit
		}
	}
	log.Infoln("read HAR file")
}
