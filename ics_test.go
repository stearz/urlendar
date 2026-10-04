package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestGenerateICSProducesCompliantCalendar(t *testing.T) {
	t.Parallel()

	event := Event{
		Title:       "Platform Meetup, Zürich; Edition",
		Start:       time.Date(2026, 11, 12, 17, 0, 0, 0, time.UTC),
		End:         time.Date(2026, 11, 12, 19, 0, 0, 0, time.UTC),
		Description: "First line\nSecond line with a backslash \\ and enough Unicode 🐙 to force folding across a physical line boundary.",
		Location:    "Werk 1; Room, 2",
		SourceURL:   "https://example.org/events/42",
	}
	generatedAt := time.Date(2026, 10, 3, 9, 30, 0, 0, time.UTC)
	calendarURL := "https://urlendar.stearz.net/v1/event?" + event.CanonicalQuery()

	got := generateICS(event, calendarURL, generatedAt)

	if !bytes.HasSuffix(got, []byte("END:VCALENDAR\r\n")) {
		t.Fatalf("calendar has wrong ending: %q", got[len(got)-30:])
	}
	if bytes.Contains(bytes.ReplaceAll(got, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatal("calendar contains bare LF line endings")
	}
	unfolded := strings.ReplaceAll(string(got), "\r\n ", "")
	for _, want := range []string{
		"VERSION:2.0\r\n",
		"PRODID:-//URLendar//URLendar v1//EN\r\n",
		"UID:" + event.UID() + "\r\n",
		"DTSTAMP:20261003T093000Z\r\n",
		"DTSTART:20261112T170000Z\r\n",
		"DTEND:20261112T190000Z\r\n",
		"SUMMARY:Platform Meetup\\, Zürich\\; Edition\r\n",
		"DESCRIPTION:First line\\nSecond line with a backslash \\\\ and enough Unicode",
		"LOCATION:Werk 1\\; Room\\, 2\r\n",
		"URL:" + calendarURL + "\r\n",
	} {
		if !strings.Contains(unfolded, want) {
			t.Errorf("calendar does not contain %q\n%s", want, unfolded)
		}
	}
	for _, line := range bytes.Split(got, []byte("\r\n")) {
		if len(line) > 75 {
			t.Errorf("physical line has %d octets: %q", len(line), line)
		}
		if !utf8.Valid(line) {
			t.Errorf("folding split UTF-8: %q", line)
		}
	}
}

func TestGenerateICSIsDeterministicForFixedInputs(t *testing.T) {
	t.Parallel()
	event := Event{
		Title: "Event",
		Start: time.Date(2026, 11, 12, 17, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC),
	}
	at := time.Date(2026, 10, 3, 9, 30, 0, 0, time.UTC)
	first := generateICS(event, "https://urlendar.stearz.net/v1/event?"+event.CanonicalQuery(), at)
	second := generateICS(event, "https://urlendar.stearz.net/v1/event?"+event.CanonicalQuery(), at)
	if !bytes.Equal(first, second) {
		t.Fatal("same event generated different calendar bytes")
	}
}

func TestGenerateLocalizedICSDescribesSpringDSTAsDaylight(t *testing.T) {
	t.Parallel()
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	event := Event{
		Title: "DST crossing",
		Start: time.Date(2026, 3, 29, 0, 30, 0, 0, time.UTC),
		End:   time.Date(2026, 3, 29, 3, 30, 0, 0, time.UTC),
	}
	got := string(generateICSInLocation(event, "https://urlendar.stearz.net/v1/event?"+event.CanonicalQuery(), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), berlin))
	want := "BEGIN:DAYLIGHT\r\nDTSTART:20260329T020000\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:+0200\r\n"
	if !strings.Contains(got, want) {
		t.Fatalf("calendar does not describe the spring DST transition as daylight\n%s", got)
	}
}
