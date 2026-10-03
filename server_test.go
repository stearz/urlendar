package main

import (
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEventPageRendersMetadataAndEscapesInput(t *testing.T) {
	t.Parallel()

	server := newServer(appConfig{
		PublicOrigin: "https://urlendar.stearz.net",
		Now:          func() time.Time { return time.Date(2026, 10, 3, 9, 30, 0, 0, time.UTC) },
	})
	query := url.Values{
		"title":       {`Meetup <script>alert("x")</script>`},
		"start":       {"2026-11-12T17:00:00Z"},
		"end":         {"2026-11-12T19:00:00Z"},
		"description": {"A practical event"},
		"location":    {"Berlin"},
	}.Encode()

	request := httptest.NewRequest(http.MethodGet, "/v1/event?"+query, nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{
		`property="og:title"`,
		`property="og:description"`,
		`property="og:url"`,
		`href="/v1/event.ics?`,
		`href="/v1/google?`,
		`href="/v1/outlook?`,
		`Meetup &lt;script&gt;alert`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q\n%s", want, body)
		}
	}
	if strings.Contains(body, `<script>alert("x")</script>`) {
		t.Fatal("hostile title was rendered as HTML")
	}
	if strings.Contains(body, "/v1/event.ics?end%3d") {
		t.Fatal("calendar action encoded the complete query as one URL component")
	}
	assertSecurityHeaders(t, response.Header())
}

func TestCreateRedirectsLocalTimeToCanonicalEvent(t *testing.T) {
	t.Parallel()
	server := newServer(appConfig{PublicOrigin: "https://urlendar.stearz.net", Now: time.Now})
	query := url.Values{
		"title": {"Meetup"},
		"start": {"2026-11-12T18:00"},
		"end":   {"2026-11-12T20:00"},
		"tz":    {"Europe/Berlin"},
	}.Encode()
	request := httptest.NewRequest(http.MethodGet, "/create?"+query, nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	location := response.Header().Get("Location")
	if !strings.HasPrefix(location, "/v1/event?") {
		t.Fatalf("location = %q", location)
	}
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := parsed.Query().Get("start"), "2026-11-12T17:00:00Z"; got != want {
		t.Fatalf("start = %q, want %q", got, want)
	}
}

func TestICSAndProviderRoutes(t *testing.T) {
	t.Parallel()
	server := newServer(appConfig{
		PublicOrigin: "https://urlendar.stearz.net",
		Now:          func() time.Time { return time.Date(2026, 10, 3, 9, 30, 0, 0, time.UTC) },
	})
	query := url.Values{
		"title": {"Meetup"},
		"start": {"2026-11-12T17:00:00Z"},
		"end":   {"2026-11-12T19:00:00Z"},
	}.Encode()

	t.Run("ICS", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/v1/event.ics?"+query, nil)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d", response.Code)
		}
		if got := response.Header().Get("Content-Type"); got != "text/calendar; charset=utf-8" {
			t.Fatalf("content type = %q", got)
		}
		_, parameters, err := mime.ParseMediaType(response.Header().Get("Content-Disposition"))
		if err != nil {
			t.Fatalf("invalid content disposition: %v", err)
		}
		if parameters["filename"] != "event.ics" {
			t.Fatalf("filename = %q", parameters["filename"])
		}
		if !strings.Contains(response.Body.String(), "BEGIN:VCALENDAR\r\n") {
			t.Fatal("response is not an ICS calendar")
		}
	})

	for _, tt := range []struct {
		path string
		host string
	}{
		{path: "/v1/google", host: "calendar.google.com"},
		{path: "/v1/outlook", host: "outlook.office.com"},
	} {
		t.Run(tt.host, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, tt.path+"?"+query, nil)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != http.StatusFound {
				t.Fatalf("status = %d", response.Code)
			}
			destination, err := url.Parse(response.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			if destination.Host != tt.host {
				t.Fatalf("redirect host = %q, want %q", destination.Host, tt.host)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("redirect is cacheable")
			}
		})
	}
}

func TestProtocolBehavior(t *testing.T) {
	t.Parallel()
	server := newServer(appConfig{PublicOrigin: "https://urlendar.stearz.net", Now: time.Now})

	tests := []struct {
		name   string
		method string
		target string
		status int
	}{
		{name: "health", method: http.MethodGet, target: "/healthz", status: http.StatusOK},
		{name: "head", method: http.MethodHead, target: "/", status: http.StatusOK},
		{name: "method", method: http.MethodPost, target: "/", status: http.StatusMethodNotAllowed},
		{name: "missing", method: http.MethodGet, target: "/missing", status: http.StatusNotFound},
		{name: "invalid event", method: http.MethodGet, target: "/v1/event?title=x", status: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.target, nil)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != tt.status {
				body, _ := io.ReadAll(response.Result().Body)
				t.Fatalf("status = %d, want %d, body = %s", response.Code, tt.status, body)
			}
			assertSecurityHeaders(t, response.Header())
		})
	}
}

func TestMalformedQueryEncodingIsRejected(t *testing.T) {
	t.Parallel()
	server := newServer(appConfig{PublicOrigin: "https://urlendar.stearz.net", Now: time.Now})
	request := httptest.NewRequest(http.MethodGet, "/v1/event?title=Good&start=2026-11-12T17%3A00%3A00Z&end=2026-11-12T18%3A00%3A00Z", nil)
	request.URL.RawQuery += "&title=%zz"
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestMalformedSemicolonQueryIsRejectedAccurately(t *testing.T) {
	t.Parallel()
	server := newServer(appConfig{PublicOrigin: "https://urlendar.stearz.net", Now: time.Now})
	request := httptest.NewRequest(http.MethodGet, "/v1/event?title=Good;bad=x&start=2026-11-12T17:00:00Z&end=2026-11-12T18:00:00Z", nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "malformed query syntax") {
		t.Fatalf("problem detail = %s", response.Body.String())
	}
}

func TestCreateRejectsLocalTimeZone(t *testing.T) {
	t.Parallel()
	server := newServer(appConfig{PublicOrigin: "https://urlendar.stearz.net", Now: time.Now})
	request := httptest.NewRequest(http.MethodGet, "/create?title=Meetup&start=2026-11-12T17:00&end=2026-11-12T18:00&tz=Local", nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestCreateRejectsDSTGapAndFold(t *testing.T) {
	t.Parallel()
	server := newServer(appConfig{PublicOrigin: "https://urlendar.stearz.net", Now: time.Now})
	tests := []struct {
		name  string
		start string
		end   string
		zone  string
	}{
		{name: "gap", start: "2026-03-29T02:30", end: "2026-03-29T04:00", zone: "Europe/Berlin"},
		{name: "fold", start: "2026-10-25T02:15", end: "2026-10-25T04:00", zone: "Europe/Berlin"},
		{name: "large historical fold", start: "1969-09-30T12:00", end: "1969-10-02T12:00", zone: "Pacific/Kwajalein"},
		{name: "sub-minute historical gap", start: "1937-07-01T00:00", end: "1937-07-01T01:00", zone: "Europe/Amsterdam"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := url.Values{
				"title": {"Meetup"},
				"start": {tt.start},
				"end":   {tt.end},
				"tz":    {tt.zone},
			}.Encode()
			request := httptest.NewRequest(http.MethodGet, "/create?"+query, nil)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, location = %q", response.Code, response.Header().Get("Location"))
			}
		})
	}
}

func TestCreatePreservesHistoricalOffsetSeconds(t *testing.T) {
	t.Parallel()
	server := newServer(appConfig{PublicOrigin: "https://urlendar.stearz.net", Now: time.Now})
	query := url.Values{
		"title": {"Historical event"},
		"start": {"1919-01-01T12:00"},
		"end":   {"1919-01-01T13:00"},
		"tz":    {"Europe/Amsterdam"},
	}.Encode()
	request := httptest.NewRequest(http.MethodGet, "/create?"+query, nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	destination, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := destination.Query().Get("start"), "1919-01-01T11:40:28Z"; got != want {
		t.Fatalf("start = %q, want %q", got, want)
	}
}

func assertSecurityHeaders(t *testing.T, header http.Header) {
	t.Helper()
	for _, key := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy", "Permissions-Policy"} {
		if header.Get(key) == "" {
			t.Errorf("missing %s", key)
		}
	}
}

func TestEventEndpointAcceptsMaximumEncodedSchemaSize(t *testing.T) {
	handler := newServer(appConfig{PublicOrigin: "https://urlendar.stearz.net", Now: time.Now})
	sourceURL := "https://example.org/" + strings.Repeat("a", maxURLBytes-len("https://example.org/"))
	query := url.Values{
		"title":       {strings.Repeat("t", maxTitleBytes)},
		"start":       {"2026-11-12T17:00:00Z"},
		"end":         {"2026-11-12T18:00:00Z"},
		"description": {strings.Repeat("🙂", maxDescriptionBytes/len("🙂"))},
		"location":    {strings.Repeat("l", maxLocationBytes)},
		"url":         {sourceURL},
	}.Encode()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler, MaxHeaderBytes: maxRequestHeaderBytes}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	response, err := http.Get("http://" + listener.Addr().String() + "/v1/event?" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d for %d-byte encoded query, body = %s", response.StatusCode, len(query), body)
	}
}

func TestPrimaryButtonMeetsWCAGContrast(t *testing.T) {
	text := cssColor(t, "text")
	accent := cssColor(t, "accent")
	if ratio := contrastRatio(text, accent); ratio < 4.5 {
		t.Fatalf("button contrast ratio = %.2f, want at least 4.5", ratio)
	}
}

func cssColor(t *testing.T, name string) [3]float64 {
	t.Helper()
	marker := "--" + name + ":#"
	start := strings.Index(styleCSS, marker)
	if start < 0 {
		t.Fatalf("CSS variable %s not found", name)
	}
	start += len(marker)
	hex := styleCSS[start : start+6]
	var color [3]float64
	for index := range color {
		component, err := strconv.ParseUint(hex[index*2:index*2+2], 16, 8)
		if err != nil {
			t.Fatalf("parse CSS variable %s: %v", name, err)
		}
		color[index] = float64(component) / 255
	}
	return color
}

func contrastRatio(first, second [3]float64) float64 {
	firstLuminance := relativeLuminance(first)
	secondLuminance := relativeLuminance(second)
	lighter, darker := firstLuminance, secondLuminance
	if lighter < darker {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05)
}

func relativeLuminance(color [3]float64) float64 {
	for index, component := range color {
		if component <= 0.04045 {
			color[index] = component / 12.92
		} else {
			color[index] = math.Pow((component+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*color[0] + 0.7152*color[1] + 0.0722*color[2]
}
