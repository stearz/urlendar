# URLendar

**The event is the link.**

URLendar creates calendar events whose complete data lives in the URL. It has no accounts, database, signatures, cookies, tracking, or built-in URL shortener.

## Use it

Open the form, enter an event, and share the resulting URL. Visitors can download an RFC 5545 `.ics` file or open the event in Google Calendar or Outlook. Event pages show times in the visitor's browser-local time zone; the original UTC value remains present in the HTML as a no-JavaScript fallback.

The header links to the [URLendar GitHub repository](https://github.com/stearz/urlendar).

## URL API

The stable v1 endpoint is:

```text
GET /v1/event?title=...&start=...&end=...
```

Required parameters:

- `title`: 1–200 UTF-8 bytes
- `start`: RFC 3339 timestamp with an explicit offset
- `end`: RFC 3339 timestamp after `start`

Optional parameters:

- `description`: up to 4,000 UTF-8 bytes
- `location`: up to 500 UTF-8 bytes
- `url`: absolute HTTPS event URL, up to 2,048 UTF-8 bytes

`start` and `end` are converted to UTC and truncated to whole seconds before
their order is checked. The normalized `end` must be after the normalized
`start`, and an event may last at most 366 days. The complete percent-encoded
query string may be at most 24 KiB.

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
- `PUBLIC_ORIGIN`: canonical absolute HTTP(S) origin without credentials, path,
  query, fragment, or control characters; default `https://urlendar.stearz.net`

## Container

```sh
docker build -t urlendar .
docker run --rm -p 8080:8080 urlendar
```

The image runs as UID/GID `65532`, writes no files, and exposes `/healthz` for health checks.

## Releases

`VERSION` is the next stable semantic version. Merging a change to that file into `main` publishes a multi-architecture GHCR image tagged with that exact version and creates the matching GitHub release (`v<version>`). Versions are immutable: the workflow refuses to reuse an existing release tag.

For the first release, `VERSION` is `0.1.0`; GitOps can reference `ghcr.io/stearz/urlendar:0.1.0` after the release workflow has completed.

## Privacy model

Event data is public because it is part of the URL. URLs can appear in browser history, logs, chat systems, calendar providers, and social-network preview caches. URLendar itself deliberately does not persist event data or log query strings.
