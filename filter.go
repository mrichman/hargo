package hargo

import (
	"fmt"
	"regexp"
	"strings"
)

// EntryFilter selects a subset of HAR entries. A zero EntryFilter matches
// everything, and every criterion that is set must match.
type EntryFilter struct {
	// URL is an unanchored regular expression matched against the full request
	// URL. An empty URL matches any URL.
	//
	// A regular expression rather than a glob: a glob's * does not cross /, so
	// the intuitive "*.js" would match no full URL at all.
	URL string

	// Method restricts the selection to these HTTP methods, compared
	// case-insensitively. An empty Method matches any method.
	Method []string

	// Status restricts the selection to these response status codes, as recorded
	// in the HAR. An empty Status matches any status.
	//
	// This is always the recorded status, never a live one: the operations that
	// replay entries cannot know a status before sending the request, and the
	// operations that only read a HAR have nothing else available.
	Status []int

	re       *regexp.Regexp
	compiled bool
}

// IsZero reports whether f selects everything.
func (f *EntryFilter) IsZero() bool {
	return f.URL == "" && len(f.Method) == 0 && len(f.Status) == 0
}

// compile prepares f for use, reporting an unusable URL pattern.
//
// Every entry point calls this before filtering and returns its error, so a
// mistyped pattern is reported rather than silently matching nothing.
func (f *EntryFilter) compile() error {
	if f.compiled {
		return nil
	}

	if f.URL != "" {
		re, err := regexp.Compile(f.URL)
		if err != nil {
			return fmt.Errorf("invalid URL filter %q: %w", f.URL, err)
		}
		f.re = re
	}

	f.compiled = true
	return nil
}

// Match reports whether e satisfies every criterion set on f.
//
// compile must have been called first; an uncompiled URL pattern is treated as
// matching nothing rather than panicking.
func (f *EntryFilter) Match(e Entry) bool {
	if f.URL != "" {
		if f.re == nil || !f.re.MatchString(e.Request.URL) {
			return false
		}
	}

	if len(f.Method) > 0 && !matchesAnyMethod(e.Request.Method, f.Method) {
		return false
	}

	if len(f.Status) > 0 && !containsInt(f.Status, e.Response.Status) {
		return false
	}

	return true
}

// matchesAnyMethod reports whether method equals any of want, ignoring case.
func matchesAnyMethod(method string, want []string) bool {
	for _, w := range want {
		if strings.EqualFold(method, w) {
			return true
		}
	}
	return false
}

// containsInt reports whether want contains status.
func containsInt(want []int, status int) bool {
	for _, w := range want {
		if w == status {
			return true
		}
	}
	return false
}

// filterEntries returns the entries of h that f selects.
//
// The slice is filtered rather than skipped entry by entry inside a replay loop,
// so that a failure tally counts only the entries actually attempted.
func filterEntries(entries []Entry, f *EntryFilter) []Entry {
	if f.IsZero() {
		return entries
	}

	kept := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if f.Match(e) {
			kept = append(kept, e)
		}
	}
	return kept
}
