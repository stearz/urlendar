package main

import (
	"bytes"
	"strings"
	"time"
	"unicode/utf8"
)

func generateICS(event Event, calendarURL string, generatedAt time.Time) []byte {
	var logical []string
	logical = append(logical,
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//URLendar//URLendar v1//EN",
		"CALSCALE:GREGORIAN",
		"BEGIN:VEVENT",
		"UID:"+event.UID(),
		"DTSTAMP:"+formatICSTime(generatedAt),
		"DTSTART:"+formatICSTime(event.Start),
		"DTEND:"+formatICSTime(event.End),
		"SUMMARY:"+escapeICSText(event.Title),
	)
	if event.Description != "" || event.SourceURL != "" {
		description := event.Description
		if event.SourceURL != "" {
			if description != "" {
				description += "\n\n"
			}
			description += "Source: " + event.SourceURL
		}
		logical = append(logical, "DESCRIPTION:"+escapeICSText(description))
	}
	if event.Location != "" {
		logical = append(logical, "LOCATION:"+escapeICSText(event.Location))
	}
	logical = append(logical,
		"URL:"+calendarURL,
		"END:VEVENT",
		"END:VCALENDAR",
	)

	var out bytes.Buffer
	for _, line := range logical {
		for _, folded := range foldICSLine(line) {
			out.WriteString(folded)
			out.WriteString("\r\n")
		}
	}
	return out.Bytes()
}

func formatICSTime(value time.Time) string {
	return value.UTC().Format("20060102T150405Z")
}

func escapeICSText(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, ";", "\\;")
	value = strings.ReplaceAll(value, ",", "\\,")
	return value
}

func foldICSLine(line string) []string {
	if len(line) <= 75 {
		return []string{line}
	}

	remaining := line
	limit := 75
	var folded []string
	for len(remaining) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(remaining[cut]) {
			cut--
		}
		if cut == 0 {
			cut = limit
		}
		folded = append(folded, remaining[:cut])
		remaining = " " + remaining[cut:]
		limit = 75
	}
	folded = append(folded, remaining)
	return folded
}
