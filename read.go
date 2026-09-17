package hargo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

// ReadOptions controls how ReadStream decodes and selects entries. The zero
// value streams every entry and produces no output.
type ReadOptions struct {
	// Filter selects which entries to deliver. A zero Filter selects all of them.
	Filter EntryFilter

	// Logger receives diagnostics. A nil Logger discards them.
	Logger *slog.Logger
}

// ReadStream decodes the entries of a HAR document and sends the selected ones on
// entries, replaying r from the start each time it reaches the end so that a load
// test can run for longer than the recording. It closes entries before returning.
//
// ReadStream returns ctx.Err() when ctx is cancelled, nil when r holds no usable
// entries, and a descriptive error when r cannot be decoded or when a filter
// selects none of the entries it did find. Streaming avoids holding the whole
// document in memory.
//
// https://golang.org/pkg/encoding/json/#example_Decoder_Decode_stream
func ReadStream(ctx context.Context, r io.ReadSeeker, entries chan<- Entry, opts ReadOptions) error {
	log := loggerOrDiscard(opts.Logger)

	defer close(entries)

	if err := opts.Filter.compile(); err != nil {
		return err
	}

	for {
		// The outer loop replays the document indefinitely, so it needs its own
		// cancellation check; without one an input that yields no entries spins
		// here forever and never observes cancellation.
		if err := ctx.Err(); err != nil {
			return err
		}

		log.Debug("reading HAR file")
		decoder := json.NewDecoder(NewReader(r))

		if err := scanToEntries(decoder); err != nil {
			return fmt.Errorf("cannot read HAR entries: %w", err)
		}

		// Consume the opening bracket of the entries array.
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("cannot read HAR entries: %w", err)
		}

		// usable counts entries that could have been sent, so that a filter which
		// selects nothing is distinguishable from a HAR with nothing in it.
		usable, sent := 0, 0
		for decoder.More() {
			var e Entry
			if err := decoder.Decode(&e); err != nil {
				return fmt.Errorf("cannot decode HAR entry: %w", err)
			}

			if len(e.Request.URL) == 0 {
				continue
			}
			usable++

			// Filtering happens here rather than in the consumer: a consumer-side
			// filter would leave sent non-zero, so the replay loop below would spin
			// re-reading and re-seeking the file while discarding everything.
			if !opts.Filter.Match(e) {
				continue
			}

			// A plain send would block forever if the consumer stopped reading,
			// leaving this goroutine leaked after cancellation.
			select {
			case entries <- e:
				sent++
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// A pass that produced nothing will produce nothing on every
		// subsequent pass either, so replaying it just burns CPU.
		if sent == 0 {
			if usable > 0 {
				// Ending silently here would look like a HAR with no entries.
				return fmt.Errorf("filter selected none of the %d entries in the HAR", usable)
			}
			log.Warn("HAR file contains no usable entries")
			return nil
		}

		log.Debug("read HAR file", "entries", sent)

		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("cannot rewind HAR file to replay it: %w", err)
		}
	}
}

// scanToEntries advances dec so that the next token is the opening bracket of the
// log's entries array.
//
// json.Decoder.Token cannot distinguish an object key from a string value: both
// arrive as a string. Matching the first token equal to "entries" therefore also
// matched a value, so a HAR containing something like {"comment":"entries"} before
// the real key failed the whole run with a misleading decode error. It also
// matched an unrelated nested key of the same name, such as one inside a page
// object. This tracks the container nesting and the enclosing key so that only
// the log's own entries array matches.
func scanToEntries(dec *json.Decoder) error {
	// inObject reports, for each open container, whether it is an object.
	var inObject []bool
	// keyOf holds the most recent key seen in each open container, which is only
	// meaningful for objects.
	var keyOf []string
	// expectKey reports whether the next string token is a key rather than a
	// value. In an object, keys and values alternate.
	expectKey := false

	push := func(isObject bool) {
		inObject = append(inObject, isObject)
		keyOf = append(keyOf, "")
		expectKey = isObject
	}
	pop := func() {
		if len(inObject) > 0 {
			inObject = inObject[:len(inObject)-1]
			keyOf = keyOf[:len(keyOf)-1]
		}
		// A closed container is a completed value, so in an enclosing object the
		// next token is a key again.
		expectKey = len(inObject) > 0 && inObject[len(inObject)-1]
	}

	for {
		t, err := dec.Token()
		if err != nil {
			return err
		}

		if d, ok := t.(json.Delim); ok {
			switch d {
			case '{':
				push(true)
			case '[':
				push(false)
			case '}', ']':
				pop()
			}
			continue
		}

		if key, ok := t.(string); ok && expectKey {
			depth := len(inObject)
			keyOf[depth-1] = key

			// The entries array belongs to the log object, so the enclosing
			// object must itself be the value of a "log" key.
			if key == "entries" && depth >= 2 && keyOf[depth-2] == "log" {
				return nil
			}

			// The next token is this key's value.
			expectKey = false
			continue
		}

		// Any other completed scalar value; in an object a key follows.
		expectKey = len(inObject) > 0 && inObject[len(inObject)-1]
	}
}
