package hargo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
)

// HARVersion is the only HAR specification version this package supports.
const HARVersion = "1.2"

// Validate reports whether r contains a usable HAR log, returning a descriptive
// error when it does not.
//
// It checks that the document parses, declares a supported version, and carries
// the fields the rest of this package relies on: an entries array, and for each
// entry a parseable startedDateTime and a request with a method and a usable URL.
// A document that passes can be replayed by [Run] without surprises.
func Validate(r io.Reader) error {
	dec := json.NewDecoder(NewReader(r))

	// Log is a pointer so that an absent log object is distinguishable from an
	// empty one, and Entries is checked for nil for the same reason.
	var doc struct {
		Log *Log `json:"log"`
	}

	if err := dec.Decode(&doc); err != nil {
		// errors.As rather than a type switch, so a wrapped decode error is
		// still classified correctly.
		var ute *json.UnmarshalTypeError
		var se *json.SyntaxError
		switch {
		case errors.As(err, &ute):
			return fmt.Errorf("invalid HAR: cannot unmarshal %s into %s at offset %d: %w",
				ute.Value, ute.Type, ute.Offset, err)
		case errors.As(err, &se):
			return fmt.Errorf("invalid HAR: malformed JSON at offset %d: %w", se.Offset, err)
		default:
			return fmt.Errorf("invalid HAR: %w", err)
		}
	}

	// Decode stops after one JSON value, so anything following it would otherwise
	// pass unnoticed.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("invalid HAR: unexpected data after the JSON document")
	}

	if doc.Log == nil {
		return errors.New(`invalid HAR: no "log" object`)
	}
	log := doc.Log

	if log.Version != HARVersion {
		return fmt.Errorf("unsupported HAR version %q, want %q", log.Version, HARVersion)
	}

	if log.Entries == nil {
		return errors.New(`invalid HAR: log has no "entries" array`)
	}

	for i, entry := range log.Entries {
		if err := validateEntry(entry); err != nil {
			return fmt.Errorf("invalid HAR: entry %d: %w", i, err)
		}
	}

	return nil
}

// validateEntry checks the entry fields this package depends on.
func validateEntry(entry Entry) error {
	// Run orders and paces entries by this timestamp, so an unparseable one is
	// worth reporting up front rather than discovering mid-replay.
	if entry.StartedDateTime == "" {
		return errors.New("no startedDateTime")
	}
	if _, err := ParseEntryTime(entry.StartedDateTime); err != nil {
		return err
	}

	if entry.Request.Method == "" {
		return errors.New("request has no method")
	}
	if entry.Request.URL == "" {
		return errors.New("request has no url")
	}

	// A URL that cannot be parsed would fail per-entry during a replay.
	u, err := url.Parse(entry.Request.URL)
	if err != nil {
		return fmt.Errorf("request url %q: %w", entry.Request.URL, err)
	}
	if u.Scheme == "" {
		return fmt.Errorf("request url %q has no scheme", entry.Request.URL)
	}

	return nil
}
