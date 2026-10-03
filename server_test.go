package main

import (
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
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
