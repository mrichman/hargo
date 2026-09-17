package hargo

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
)

// HarVersion is the only HAR specification version this package supports.
const HarVersion = "1.2"

// Validate validates the format of a .har file. It reports whether the input
// parses as a HAR log declaring a supported version, and returns a descriptive
// error when it does not.
func Validate(r *bufio.Reader) (bool, error) {
	dec := json.NewDecoder(r)
	var har Har

	if err := dec.Decode(&har); err != nil {
		// errors.As rather than a type switch, so a wrapped decode error is
		// still classified correctly.
		var ute *json.UnmarshalTypeError
		var se *json.SyntaxError
		switch {
		case errors.As(err, &ute):
			return false, fmt.Errorf("invalid HAR: cannot unmarshal %s into %s at offset %d: %w",
				ute.Value, ute.Type, ute.Offset, err)
		case errors.As(err, &se):
			return false, fmt.Errorf("invalid HAR: malformed JSON at offset %d: %w", se.Offset, err)
		default:
			return false, fmt.Errorf("invalid HAR: %w", err)
		}
	}

	if har.Log.Version != HarVersion {
		return false, fmt.Errorf("unsupported HAR version %q, want %q", har.Log.Version, HarVersion)
	}

	return true, nil
}
