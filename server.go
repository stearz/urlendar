package main

import (
	"bytes"
	"encoding/json"
	"html"
	"html/template"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

const maxRawQueryBytes = 24 * 1024

type appConfig struct {
	PublicOrigin string
	Now          func() time.Time
}

type application struct {
	origin string
	now    func() time.Time
	home   *template.Template
	event  *template.Template
	mux    *http.ServeMux
}

func newServer(config appConfig) http.Handler {
	origin := strings.TrimRight(config.PublicOrigin, "/")
	if origin == "" {
		origin = "https://urlendar.stearz.net"
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	app := &application{
		origin: origin,
		now:    config.Now,
		home:   template.Must(template.New("home").Parse(homeTemplate)),
		event:  template.Must(template.New("event").Parse(eventTemplate)),
		mux:    http.NewServeMux(),
	}
	app.routes()
	return app.securityHeaders(app.methodGuard(app.mux))
}

func (a *application) routes() {
	a.mux.HandleFunc("/", a.handleHome)
	a.mux.HandleFunc("/create", a.handleCreate)
	a.mux.HandleFunc("/v1/event", a.handleEvent)
	a.mux.HandleFunc("/v1/event.ics", a.handleICS)
	a.mux.HandleFunc("/v1/google", a.handleGoogle)
	a.mux.HandleFunc("/v1/outlook", a.handleOutlook)
	a.mux.HandleFunc("/healthz", a.handleHealth)
	a.mux.HandleFunc("/static/style.css", a.handleStyle)
	a.mux.HandleFunc("/static/app.js", a.handleScript)
	a.mux.HandleFunc("/static/preview.png", a.handlePreview)
}

func (a *application) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.home.Execute(w, map[string]string{"Origin": a.origin})
}

func (a *application) handleCreate(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.RawQuery) > maxRawQueryBytes {
		a.writeProblem(w, http.StatusBadRequest, invalid("query", "query string is too long"))
		return
	}
	values, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil {
		a.writeProblem(w, http.StatusBadRequest, invalid("query", "contains malformed percent-encoding"))
		return
	}
	allowed := map[string]bool{"title": true, "start": true, "end": true, "tz": true, "description": true, "location": true, "url": true}
	for key, entries := range values {
		if !allowed[key] {
			a.writeProblem(w, http.StatusBadRequest, invalid(key, "unknown parameter"))
			return
		}
		if len(entries) != 1 {
			a.writeProblem(w, http.StatusBadRequest, invalid(key, "parameter must occur exactly once"))
			return
		}
	}
	zoneName := strings.TrimSpace(values.Get("tz"))
	zone, err := time.LoadLocation(zoneName)
	if err != nil || zoneName == "" {
		a.writeProblem(w, http.StatusBadRequest, invalid("tz", "must be a valid IANA time zone"))
		return
	}
	start, validationErr := parseLocalFormTime(values.Get("start"), zone)
	if validationErr != nil {
		validationErr.Parameter = "start"
		a.writeProblem(w, http.StatusBadRequest, validationErr)
		return
	}
	end, validationErr := parseLocalFormTime(values.Get("end"), zone)
	if validationErr != nil {
		validationErr.Parameter = "end"
		a.writeProblem(w, http.StatusBadRequest, validationErr)
		return
	}
	canonical := url.Values{
		"title": {values.Get("title")},
		"start": {start.UTC().Format(time.RFC3339)},
		"end":   {end.UTC().Format(time.RFC3339)},
	}
	for _, key := range []string{"description", "location", "url"} {
		if value := values.Get(key); value != "" {
			canonical.Set(key, value)
		}
	}
	event, validationErr := parseEvent(canonical)
	if validationErr != nil {
		a.writeProblem(w, http.StatusBadRequest, validationErr)
		return
	}
	http.Redirect(w, r, "/v1/event?"+event.CanonicalQuery(), http.StatusSeeOther)
}

func (a *application) handleEvent(w http.ResponseWriter, r *http.Request) {
	event, ok := a.eventFromRequest(w, r)
	if !ok {
		return
	}
	canonicalPath := "/v1/event?" + event.CanonicalQuery()
	data := struct {
		Event        Event
		StartDisplay string
		EndDisplay   string
		CanonicalURL string
		ICSAttr      template.HTMLAttr
		GoogleAttr   template.HTMLAttr
		OutlookAttr  template.HTMLAttr
		ImageURL     string
		Description  string
	}{
		Event:        event,
		StartDisplay: event.Start.Format("Monday, 2 January 2006 · 15:04 UTC"),
		EndDisplay:   event.End.Format("Monday, 2 January 2006 · 15:04 UTC"),
		CanonicalURL: a.origin + canonicalPath,
		ICSAttr:      actionHref("/v1/event.ics", event),
		GoogleAttr:   actionHref("/v1/google", event),
		OutlookAttr:  actionHref("/v1/outlook", event),
		ImageURL:     a.origin + "/static/preview.png",
		Description:  previewDescription(event),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.event.Execute(w, data)
}

func (a *application) handleICS(w http.ResponseWriter, r *http.Request) {
	event, ok := a.eventFromRequest(w, r)
	if !ok {
		return
	}
	calendarURL := a.origin + "/v1/event?" + event.CanonicalQuery()
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="event.ics"`)
	_, _ = w.Write(generateICS(event, calendarURL, a.now().UTC()))
}

func (a *application) handleGoogle(w http.ResponseWriter, r *http.Request) {
	event, ok := a.eventFromRequest(w, r)
	if !ok {
		return
	}
	details := providerDescription(event, a.origin+"/v1/event?"+event.CanonicalQuery())
	destination := url.URL{Scheme: "https", Host: "calendar.google.com", Path: "/calendar/render"}
	query := url.Values{
		"action":  {"TEMPLATE"},
		"text":    {event.Title},
		"dates":   {formatICSTime(event.Start) + "/" + formatICSTime(event.End)},
		"details": {details},
	}
	if event.Location != "" {
		query.Set("location", event.Location)
	}
	destination.RawQuery = query.Encode()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, destination.String(), http.StatusFound)
}

func (a *application) handleOutlook(w http.ResponseWriter, r *http.Request) {
	event, ok := a.eventFromRequest(w, r)
	if !ok {
		return
	}
	details := providerDescription(event, a.origin+"/v1/event?"+event.CanonicalQuery())
	destination := url.URL{Scheme: "https", Host: "outlook.office.com", Path: "/calendar/0/deeplink/compose"}
	query := url.Values{
		"path":    {"/calendar/action/compose"},
		"rru":     {"addevent"},
		"subject": {event.Title},
		"startdt": {event.Start.Format(time.RFC3339)},
		"enddt":   {event.End.Format(time.RFC3339)},
		"body":    {details},
	}
	if event.Location != "" {
		query.Set("location", event.Location)
	}
	destination.RawQuery = query.Encode()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, destination.String(), http.StatusFound)
}

func (a *application) eventFromRequest(w http.ResponseWriter, r *http.Request) (Event, bool) {
	if len(r.URL.RawQuery) > maxRawQueryBytes {
		a.writeProblem(w, http.StatusBadRequest, invalid("query", "query string is too long"))
		return Event{}, false
	}
	values, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil {
		a.writeProblem(w, http.StatusBadRequest, invalid("query", "contains malformed percent-encoding"))
		return Event{}, false
	}
	event, err := parseEvent(values)
	if err != nil {
		a.writeProblem(w, http.StatusBadRequest, err)
		return Event{}, false
	}
	return event, true
}

func (a *application) writeProblem(w http.ResponseWriter, status int, err *ValidationError) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"type":      "invalid-parameter",
		"parameter": err.Parameter,
		"detail":    err.Detail,
	})
}

func (a *application) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (a *application) handleStyle(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(styleCSS))
}

func (a *application) handleScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(appJS))
}

func (a *application) handlePreview(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	_, _ = w.Write(previewPNG())
}

func (a *application) methodGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *application) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		next.ServeHTTP(w, r)
	})
}

func previewDescription(event Event) string {
	if event.Description != "" {
		return truncateRunes(event.Description, 180)
	}
	return event.Start.Format("2 January 2006, 15:04 UTC")
}

func providerDescription(event Event, canonicalURL string) string {
	parts := make([]string, 0, 3)
	if event.Description != "" {
		parts = append(parts, event.Description)
	}
	if event.SourceURL != "" {
		parts = append(parts, event.SourceURL)
	}
	parts = append(parts, canonicalURL)
	return strings.Join(parts, "\n\n")
}

func actionHref(path string, event Event) template.HTMLAttr {
	return template.HTMLAttr(`href="` + html.EscapeString(path+"?"+event.CanonicalQuery()) + `"`)
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum-1]) + "…"
}

func parseLocalFormTime(value string, zone *time.Location) (time.Time, *ValidationError) {
	const layout = "2006-01-02T15:04"
	wallTime, err := time.ParseInLocation(layout, value, time.UTC)
	if err != nil {
		return time.Time{}, invalid("time", "must be a local date and time")
	}

	// A wall clock value can map to zero, one, or multiple instants. Discover
	// every UTC offset active around the date, then test each possible mapping.
	// The 72-hour window covers even historical International Date Line shifts.
	offsets := make(map[int]struct{})
	for probe := wallTime.Add(-72 * time.Hour); !probe.After(wallTime.Add(72 * time.Hour)); probe = probe.Add(15 * time.Minute) {
		_, offset := probe.In(zone).Zone()
		offsets[offset] = struct{}{}
	}
	candidates := make(map[int64]time.Time)
	for offset := range offsets {
		candidate := wallTime.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(zone)
		wallYear, wallMonth, wallDay := wallTime.Date()
		localYear, localMonth, localDay := local.Date()
		wallHour, wallMinute, wallSecond := wallTime.Clock()
		localHour, localMinute, localSecond := local.Clock()
		if localYear == wallYear && localMonth == wallMonth && localDay == wallDay &&
			localHour == wallHour && localMinute == wallMinute && localSecond == wallSecond && local.Nanosecond() == 0 {
			candidates[candidate.Unix()] = candidate
		}
	}

	switch len(candidates) {
	case 0:
		return time.Time{}, invalid("time", "does not exist in the selected time zone")
	case 1:
		for _, candidate := range candidates {
			return candidate.In(zone), nil
		}
	default:
		return time.Time{}, invalid("time", "is ambiguous in the selected time zone; use the URL API with an explicit offset")
	}
	panic("unreachable")
}

var (
	previewOnce sync.Once
	previewData []byte
)

func previewPNG() []byte {
	previewOnce.Do(func() {
		canvas := image.NewRGBA(image.Rect(0, 0, 1200, 627))
		draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{R: 20, G: 27, B: 45, A: 255}}, image.Point{}, draw.Src)
		draw.Draw(canvas, image.Rect(145, 105, 505, 465), &image.Uniform{C: color.RGBA{R: 123, G: 97, B: 255, A: 255}}, image.Point{}, draw.Src)
		draw.Draw(canvas, image.Rect(195, 165, 455, 415), &image.Uniform{C: color.RGBA{R: 245, G: 247, B: 255, A: 255}}, image.Point{}, draw.Src)
		draw.Draw(canvas, image.Rect(690, 235, 1055, 285), &image.Uniform{C: color.RGBA{R: 245, G: 247, B: 255, A: 255}}, image.Point{}, draw.Src)
		draw.Draw(canvas, image.Rect(690, 325, 940, 375), &image.Uniform{C: color.RGBA{R: 123, G: 97, B: 255, A: 255}}, image.Point{}, draw.Src)
		var encoded bytes.Buffer
		_ = png.Encode(&encoded, canvas)
		previewData = encoded.Bytes()
	})
	return previewData
}

const homeTemplate = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>URLendar — The event is the link</title><meta name="description" content="Create a calendar event that lives entirely in its URL.">
<link rel="stylesheet" href="/static/style.css"><script defer src="/static/app.js"></script></head>
<body><main><header><a class="brand" href="/">URLendar</a><p>The event is the link.</p></header>
<section class="card"><h1>Create a calendar link</h1><p>No account. No database. Event data stays in the URL.</p>
<form action="/create" method="get">
<label>Title<input name="title" required maxlength="200" autocomplete="off"></label>
<div class="grid"><label>Starts<input type="datetime-local" name="start" required></label><label>Ends<input type="datetime-local" name="end" required></label></div>
<label>Time zone<input name="tz" id="timezone" required placeholder="Europe/Berlin"></label>
<label>Location<input name="location" maxlength="500" autocomplete="off"></label>
<label>Event website<input type="url" name="url" maxlength="2048" placeholder="https://…"></label>
<label>Description<textarea name="description" maxlength="4000" rows="6"></textarea></label>
<button type="submit">Create link</button></form></section>
<footer>Open, stateless and automation-friendly. <a href="/v1/event?title=URLendar+example&amp;start=2026-12-01T17%3A00%3A00Z&amp;end=2026-12-01T18%3A00%3A00Z">See an example</a>.</footer>
</main></body></html>`

const eventTemplate = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Event.Title}} — URLendar</title><meta name="description" content="{{.Description}}"><link rel="canonical" href="{{.CanonicalURL}}">
<meta property="og:type" content="website"><meta property="og:title" content="{{.Event.Title}}"><meta property="og:description" content="{{.Description}}"><meta property="og:url" content="{{.CanonicalURL}}"><meta property="og:image" content="{{.ImageURL}}"><meta property="og:image:width" content="1200"><meta property="og:image:height" content="627">
<meta name="twitter:card" content="summary_large_image"><meta name="twitter:title" content="{{.Event.Title}}"><meta name="twitter:description" content="{{.Description}}"><meta name="twitter:image" content="{{.ImageURL}}">
<link rel="stylesheet" href="/static/style.css"></head><body><main><header><a class="brand" href="/">URLendar</a><p>The event is the link.</p></header>
<article class="card event"><p class="eyebrow">CALENDAR EVENT</p><h1>{{.Event.Title}}</h1><dl><dt>Starts</dt><dd>{{.StartDisplay}}</dd><dt>Ends</dt><dd>{{.EndDisplay}}</dd>{{if .Event.Location}}<dt>Location</dt><dd>{{.Event.Location}}</dd>{{end}}</dl>
{{if .Event.Description}}<p class="description">{{.Event.Description}}</p>{{end}}{{if .Event.SourceURL}}<p><a href="{{.Event.SourceURL}}" rel="nofollow noopener">Event website</a></p>{{end}}
<div class="actions"><a class="button primary" {{.ICSAttr}}>Download .ics</a><a class="button" {{.GoogleAttr}}>Google Calendar</a><a class="button" {{.OutlookAttr}}>Outlook</a></div></article>
<footer>This page stores nothing. Its URL contains the complete event.</footer></main></body></html>`

const styleCSS = `:root{color-scheme:dark;--bg:#10162a;--card:#18213a;--text:#f5f7ff;--muted:#aab4d0;--accent:#6548e8;--line:#2a3658;font-family:Inter,ui-sans-serif,system-ui,sans-serif}*{box-sizing:border-box}body{margin:0;background:radial-gradient(circle at top,#1a2442,var(--bg) 50%);color:var(--text);min-height:100vh}main{width:min(760px,calc(100% - 32px));margin:auto;padding:40px 0}header{display:flex;align-items:baseline;justify-content:space-between;margin-bottom:28px}.brand{font-size:1.5rem;font-weight:800;color:var(--text);text-decoration:none}header p,footer,.card>p{color:var(--muted)}.card{background:color-mix(in srgb,var(--card) 92%,transparent);border:1px solid var(--line);border-radius:20px;padding:clamp(24px,5vw,48px);box-shadow:0 24px 80px #080d1b88}h1{font-size:clamp(2rem,6vw,3.4rem);line-height:1.05;margin:.2em 0 .35em}form{display:grid;gap:18px;margin-top:28px}label{display:grid;gap:8px;color:var(--muted);font-weight:650}.grid{display:grid;grid-template-columns:1fr 1fr;gap:16px}input,textarea{width:100%;border:1px solid var(--line);border-radius:10px;background:#0f172a;color:var(--text);padding:12px 14px;font:inherit}textarea{resize:vertical}button,.button{border:1px solid var(--line);border-radius:10px;background:#202b49;color:var(--text);padding:13px 18px;font:inherit;font-weight:750;text-decoration:none;text-align:center;cursor:pointer}button,.primary{background:var(--accent);border-color:var(--accent)}footer{margin-top:26px;font-size:.9rem;text-align:center}a{color:#bcb1ff}.eyebrow{font-size:.75rem;font-weight:800;letter-spacing:.15em;color:#bcb1ff!important}.event dl{display:grid;grid-template-columns:90px 1fr;gap:10px;margin:28px 0}.event dt{color:var(--muted)}.event dd{margin:0}.description{white-space:pre-wrap;color:var(--text)!important;line-height:1.6}.actions{display:grid;grid-template-columns:repeat(3,1fr);gap:10px;margin-top:30px}@media(max-width:620px){header{display:block}.grid,.actions{grid-template-columns:1fr}.event dl{grid-template-columns:1fr}.event dt{margin-top:8px}}`

const appJS = `const zone=document.querySelector('#timezone');if(zone&&!zone.value){zone.value=Intl.DateTimeFormat().resolvedOptions().timeZone||'UTC';}`
