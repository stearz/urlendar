package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxTitleBytes       = 200
	maxDescriptionBytes = 4000
	maxLocationBytes    = 500
	maxURLBytes         = 2048
	maxEventDuration    = 366 * 24 * time.Hour
)

type Event struct {
	Title       string
	Start       time.Time
	End         time.Time
	Description string
	Location    string
	SourceURL   string
}

type ValidationError struct {
	Parameter string `json:"parameter"`
	Detail    string `json:"detail"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Parameter, e.Detail)
}

func parseEvent(values url.Values) (Event, *ValidationError) {
	allowed := map[string]int{
		"title": maxTitleBytes, "start": 64, "end": 64,
		"description": maxDescriptionBytes, "location": maxLocationBytes, "url": maxURLBytes,
	}
	for key, entries := range values {
		limit, ok := allowed[key]
		if !ok {
			return Event{}, invalid(key, "unknown parameter")
		}
		if len(entries) != 1 {
			return Event{}, invalid(key, "parameter must occur exactly once")
		}
		if !utf8.ValidString(entries[0]) {
			return Event{}, invalid(key, "must be valid UTF-8")
		}
		if len(entries[0]) > limit {
			return Event{}, invalid(key, fmt.Sprintf("must not exceed %d UTF-8 bytes", limit))
		}
		if containsDisallowedControl(entries[0]) {
			return Event{}, invalid(key, "contains a disallowed control character")
		}
	}

	title := strings.TrimSpace(values.Get("title"))
	if title == "" {
		return Event{}, invalid("title", "is required")
	}

	start, err := parseTimestamp(values.Get("start"))
	if err != nil {
		return Event{}, invalid("start", "must be an RFC 3339 timestamp with an explicit offset")
	}
	end, err := parseTimestamp(values.Get("end"))
	if err != nil {
		return Event{}, invalid("end", "must be an RFC 3339 timestamp with an explicit offset")
	}
	start = start.UTC().Truncate(time.Second)
	end = end.UTC().Truncate(time.Second)
	if !end.After(start) {
		return Event{}, invalid("end", "must be after start after normalization to whole seconds")
	}
	if end.Sub(start) > maxEventDuration {
		return Event{}, invalid("end", "event duration must not exceed 366 days")
	}

	sourceURL := strings.TrimSpace(values.Get("url"))
	if sourceURL != "" {
		parsed, parseErr := url.Parse(sourceURL)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
			return Event{}, invalid("url", "must be an absolute HTTPS URL without credentials")
		}
	}

	return Event{
		Title:       title,
		Start:       start.UTC().Truncate(time.Second),
		End:         end.UTC().Truncate(time.Second),
		Description: strings.TrimSpace(values.Get("description")),
		Location:    strings.TrimSpace(values.Get("location")),
		SourceURL:   sourceURL,
	}, nil
}

func invalid(parameter, detail string) *ValidationError {
	return &ValidationError{Parameter: parameter, Detail: detail}
}

var strictRFC3339Timestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-](?:0\d|1\d|2[0-3]):[0-5]\d)$`)

func parseTimestamp(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	if !strictRFC3339Timestamp.MatchString(value) {
		return time.Time{}, fmt.Errorf("timestamp is not strict RFC 3339")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}

func containsDisallowedControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return true
		}
	}
	return false
}

func (e Event) Values() url.Values {
	values := url.Values{
		"title": {e.Title},
		"start": {e.Start.UTC().Format(time.RFC3339)},
		"end":   {e.End.UTC().Format(time.RFC3339)},
	}
	if e.Description != "" {
		values.Set("description", e.Description)
	}
	if e.Location != "" {
		values.Set("location", e.Location)
	}
	if e.SourceURL != "" {
		values.Set("url", e.SourceURL)
	}
	return values
}

func (e Event) CanonicalQuery() string {
	return e.Values().Encode()
}

func (e Event) UID() string {
	sum := sha256.Sum256([]byte(e.CanonicalQuery()))
	return hex.EncodeToString(sum[:]) + "@urlendar.stearz.net"
}
