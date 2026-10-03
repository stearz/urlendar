package main

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseEventCanonicalizesEquivalentInputs(t *testing.T) {
	t.Parallel()

	first, err := parseEvent(url.Values{
		"title":       {"  Platform Meetup  "},
		"start":       {"2026-11-12T18:00:00+01:00"},
		"end":         {"2026-11-12T20:00:00+01:00"},
		"description": {"Systems, humans; and links\\together"},
		"location":    {"Berlin"},
		"url":         {"https://example.org/events/42"},
	})
	if err != nil {
		t.Fatalf("parse first event: %v", err)
	}
	second, err := parseEvent(url.Values{
		"end":         {"2026-11-12T19:00:00Z"},
		"start":       {"2026-11-12T17:00:00Z"},
		"title":       {"Platform Meetup"},
		"url":         {"https://example.org/events/42"},
		"location":    {"Berlin"},
		"description": {"Systems, humans; and links\\together"},
	})
	if err != nil {
		t.Fatalf("parse second event: %v", err)
	}

	if first.CanonicalQuery() != second.CanonicalQuery() {
		t.Fatalf("canonical query differs:\n%s\n%s", first.CanonicalQuery(), second.CanonicalQuery())
	}
	if first.UID() != second.UID() {
		t.Fatalf("UID differs: %q != %q", first.UID(), second.UID())
	}
	if got, want := first.Start, time.Date(2026, 11, 12, 17, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("start = %v, want %v", got, want)
	}
}

func TestParseEventRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	valid := url.Values{
		"title": {"Event"},
		"start": {"2026-11-12T17:00:00Z"},
		"end":   {"2026-11-12T18:00:00Z"},
	}

	tests := []struct {
		name      string
		mutate    func(url.Values)
		parameter string
	}{
		{name: "missing title", mutate: func(v url.Values) { v.Del("title") }, parameter: "title"},
		{name: "duplicate title", mutate: func(v url.Values) { v["title"] = []string{"one", "two"} }, parameter: "title"},
		{name: "unknown field", mutate: func(v url.Values) { v.Set("surprise", "yes") }, parameter: "surprise"},
		{name: "invalid start", mutate: func(v url.Values) { v.Set("start", "tomorrow") }, parameter: "start"},
		{name: "non-strict RFC3339 offset", mutate: func(v url.Values) { v.Set("start", "2026-11-12T17:00:00+24:00") }, parameter: "start"},
		{name: "non-strict RFC3339 comma fraction", mutate: func(v url.Values) { v.Set("start", "2026-11-12T17:00:00,1Z") }, parameter: "start"},
		{name: "end before start", mutate: func(v url.Values) { v.Set("end", "2026-11-12T16:00:00Z") }, parameter: "end"},
		{name: "duration lost by canonicalization", mutate: func(v url.Values) {
			v.Set("start", "2026-11-12T17:00:00.1Z")
			v.Set("end", "2026-11-12T17:00:00.9Z")
		}, parameter: "end"},
		{name: "insecure source URL", mutate: func(v url.Values) { v.Set("url", "http://example.org") }, parameter: "url"},
		{name: "source URL without hostname", mutate: func(v url.Values) { v.Set("url", "https://:443/path") }, parameter: "url"},
		{name: "control character", mutate: func(v url.Values) { v.Set("location", "somewhere\x00") }, parameter: "location"},
		{name: "title too long", mutate: func(v url.Values) { v.Set("title", strings.Repeat("x", 201)) }, parameter: "title"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := cloneValues(valid)
			tt.mutate(values)
			_, err := parseEvent(values)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if err.Parameter != tt.parameter {
				t.Fatalf("parameter = %q, want %q", err.Parameter, tt.parameter)
			}
		})
	}
}

func cloneValues(values url.Values) url.Values {
	copy := make(url.Values, len(values))
	for key, entries := range values {
		copy[key] = append([]string(nil), entries...)
	}
	return copy
}

func TestParseEventAppliesUTF8ByteLimits(t *testing.T) {
	values := url.Values{
		"title": {strings.Repeat("🙂", 50)},
		"start": {"2026-11-12T17:00:00Z"},
		"end":   {"2026-11-12T18:00:00Z"},
	}
	if _, err := parseEvent(values); err != nil {
		t.Fatalf("200-byte title was rejected: %v", err)
	}
	values.Set("title", strings.Repeat("🙂", 51))
	if _, err := parseEvent(values); err == nil || err.Parameter != "title" {
		t.Fatalf("204-byte title error = %v", err)
	}
}
