# URLendar

**The event is the link.**

URLendar creates calendar events whose complete data lives in the URL. It has no accounts, database, signatures, cookies, tracking, or built-in URL shortener.

## Use it

Open the form, enter an event, and share the resulting URL. Visitors can download an RFC 5545 `.ics` file or open the event in Google Calendar or Outlook.

## URL API

The stable v1 endpoint is:

```text
GET /v1/event?title=...&start=...&end=...
```

Required parameters:

- `title`: 1–200 Unicode characters
- `start`: RFC 3339 timestamp with an explicit offset
- `end`: RFC 3339 timestamp after `start`

Optional parameters:

- `description`: up to 4,000 characters
- `location`: up to 500 characters
- `url`: absolute HTTPS event URL

Example:

```text
https://urlendar.stearz.net/v1/event?title=Platform+Meetup&start=2026-11-12T17%3A00%3A00Z&end=2026-11-12T19%3A00%3A00Z&location=Berlin
```

The same query can be used with:

- `/v1/event.ics`
- `/v1/google`
- `/v1/outlook`

Unknown and duplicate parameters are rejected so the v1 contract remains deterministic.

## Run locally

Requires Go 1.27 or newer:

```sh
go test ./...
go run .
```

Then open `http://localhost:8080`.

Configuration:

- `PORT`: HTTP port, default `8080`
- `PUBLIC_ORIGIN`: canonical public origin, default `https://urlendar.stearz.net`

## Container

```sh
docker build -t urlendar .
docker run --rm -p 8080:8080 urlendar
```

The image runs as UID/GID `65532`, writes no files, and exposes `/healthz` for health checks.

## Privacy model

Event data is public because it is part of the URL. URLs can appear in browser history, logs, chat systems, calendar providers, and social-network preview caches. URLendar itself deliberately does not persist event data or log query strings.
