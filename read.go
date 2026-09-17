package hargo

import (
	"encoding/json"
	"io"
	"os"

	log "github.com/sirupsen/logrus"
)

// ReadStream reads the har file as a stream and puts the entries
// on a chan for consumption. When the end of a file is reached it
// will start over until the stop signal is given.
// https://golang.org/pkg/encoding/json/#example_Decoder_Decode_stream
func ReadStream(file *os.File, entries chan Entry, stop chan bool) {
	for {
		// The outer loop replays the file indefinitely, so it needs its own
		// stop check; without one a file that yields no entries spins here
		// forever and never observes the signal.
		select {
		case <-stop:
			log.Infoln("stop reading HAR file")
			close(entries)
			return
		default:
		}

		r := NewReader(file)

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
			// Malformed input must not terminate the caller's process.
			log.Errorf("cannot read HAR entries: %v", err)
			close(entries)
			return
		}

		// read entries
		sent := 0
		for decoder.More() {
			var e Entry
			err := decoder.Decode(&e)
			if err != nil {
				log.Errorf("cannot decode HAR entry: %v", err)
				close(entries)
				return
			}
			if len(e.Request.URL) > 0 {
				entries <- e
				sent++
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

		// A pass that produced nothing will produce nothing on every
		// subsequent pass either, so replaying it just burns CPU.
		if sent == 0 {
			log.Warn("HAR file contains no usable entries")
			close(entries)
			return
		}

		log.Infoln("read HAR file")
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			log.Errorf("cannot rewind HAR file to replay it: %v", err)
			close(entries)
			return
		}
	}
}
