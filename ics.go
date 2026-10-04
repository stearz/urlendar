package main

import (
	"bytes"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

func generateICS(event Event, calendarURL string, generatedAt time.Time) []byte {
	return generateICSInLocation(event, calendarURL, generatedAt, nil)
}

func generateICSInLocation(event Event, calendarURL string, generatedAt time.Time, location *time.Location) []byte {
	localized := location != nil && location != time.UTC && location.String() != "UTC"
	var logical []string
	logical = append(logical,
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//URLendar//URLendar v1//EN",
		"CALSCALE:GREGORIAN",
	)
	if localized {
		logical = append(logical, timeZoneComponent(location, event.Start, event.End)...)
	}
	logical = append(logical,
		"BEGIN:VEVENT",
		"UID:"+event.UID(),
		"DTSTAMP:"+formatICSTime(generatedAt),
	)
	if localized {
		zoneID := location.String()
		logical = append(logical,
			"DTSTART;TZID="+zoneID+":"+formatICSLocalTime(event.Start.In(location)),
			"DTEND;TZID="+zoneID+":"+formatICSLocalTime(event.End.In(location)),
		)
	} else {
		logical = append(logical,
			"DTSTART:"+formatICSTime(event.Start),
			"DTEND:"+formatICSTime(event.End),
		)
	}
	logical = append(logical, "SUMMARY:"+escapeICSText(event.Title))
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

func timeZoneComponent(location *time.Location, start, end time.Time) []string {
	lines := []string{"BEGIN:VTIMEZONE", "TZID:" + location.String()}
	name, offset := start.In(location).Zone()
	lines = append(lines, timeZoneObservance(
		formatICSLocalTime(start.In(location)),
		offset,
		offset,
		name,
		isDaylightOffset(location, start, offset),
	)...)

	cursor := start.UTC()
	until := end.UTC()
	for cursor.Before(until) {
		probe := cursor.Add(time.Hour)
		if probe.After(until) {
			probe = until
		}
		_, nextOffset := probe.In(location).Zone()
		if nextOffset == offset {
			cursor = probe
			continue
		}
		transition := firstOffsetChange(cursor, probe, location, offset)
		nextName, resolvedOffset := transition.In(location).Zone()
		onset := formatICSOffsetTime(transition, offset)
		lines = append(lines, timeZoneObservance(
			onset,
			offset,
			resolvedOffset,
			nextName,
			isDaylightOffset(location, transition, resolvedOffset),
		)...)

		cursor = transition
		offset = resolvedOffset
	}
	return append(lines, "END:VTIMEZONE")
}

func timeZoneObservance(onset string, from, to int, name string, daylight bool) []string {
	kind := "STANDARD"
	if daylight {
		kind = "DAYLIGHT"
	}
	return []string{
		"BEGIN:" + kind,
		"DTSTART:" + onset,
		"TZOFFSETFROM:" + formatICSOffset(from),
		"TZOFFSETTO:" + formatICSOffset(to),
		"TZNAME:" + escapeICSText(name),
		"END:" + kind,
	}
}

func isDaylightOffset(location *time.Location, reference time.Time, offset int) bool {
	minimum, maximum := offset, offset
	for probe := reference.AddDate(0, -6, 0); !probe.After(reference.AddDate(0, 6, 0)); probe = probe.AddDate(0, 0, 7) {
		_, candidateOffset := probe.In(location).Zone()
		if candidateOffset < minimum {
			minimum = candidateOffset
		}
		if candidateOffset > maximum {
			maximum = candidateOffset
		}
	}
	return maximum > minimum && offset == maximum
}

func firstOffsetChange(start, end time.Time, location *time.Location, oldOffset int) time.Time {
	low := start.UTC().Unix()
	high := end.UTC().Unix()
	for high-low > 1 {
		middle := low + (high-low)/2
		_, offset := time.Unix(middle, 0).In(location).Zone()
		if offset == oldOffset {
			low = middle
		} else {
			high = middle
		}
	}
	return time.Unix(high, 0).UTC()
}

func formatICSLocalTime(value time.Time) string {
	return value.Format("20060102T150405")
}

func formatICSOffsetTime(instant time.Time, offset int) string {
	return instant.UTC().Add(time.Duration(offset) * time.Second).Format("20060102T150405")
}

func formatICSOffset(offset int) string {
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	hours := offset / 3600
	minutes := (offset % 3600) / 60
	seconds := offset % 60
	if seconds == 0 {
		return fmt.Sprintf("%s%02d%02d", sign, hours, minutes)
	}
	return fmt.Sprintf("%s%02d%02d%02d", sign, hours, minutes, seconds)
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
