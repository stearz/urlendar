# URLendar

**The event is the link.**

URLendar turns calendar events into shareable URLs. All event data is stored in
the URL itself, so the service needs no accounts or database and does not use
cookies or tracking.

## Create and share an event

1. Open [urlendar.stearz.net](https://urlendar.stearz.net).
2. Enter the title, start and end time, and time zone. Location, event website,
   and description are optional.
3. Select **Create link**.
4. Copy the resulting page URL and send it to your guests.

Anyone who opens the link sees the event in their browser's local time zone and
can:

- download an RFC 5545 `.ics` file for most calendar applications;
- add the event to Google Calendar; or
- add the event to Outlook.

The generated link is the event record: editing a query parameter creates a
different event, and losing the link means losing access to it. URLendar does
not provide invitations, guest lists, updates, reminders, or a URL shortener.

> [!IMPORTANT]
> Treat an event link as public. Its title, times, description, location, and
> source URL are readable in the URL and may appear in browser history, server
> logs, messages, calendar providers, and link-preview caches. Do not put
> confidential information in an event.

## Build links programmatically

Create an event page with a `GET` request to `/v1/event`:

```text
https://urlendar.stearz.net/v1/event?title=Platform+Meetup&start=2026-11-12T17%3A00%3A00Z&end=2026-11-12T19%3A00%3A00Z&location=Berlin
```

The query string must be URL-encoded. For example, with JavaScript:

```js
const event = new URL("https://urlendar.stearz.net/v1/event");
event.search = new URLSearchParams({
  title: "Platform Meetup",
  start: "2026-11-12T18:00:00+01:00",
  end: "2026-11-12T20:00:00+01:00",
  location: "Berlin",
  description: "An evening for platform engineers",
});

console.log(event.href);
```

Use the same query parameters with a different endpoint to choose the result:

| Endpoint | Result |
| --- | --- |
| `/v1/event` | Human-readable event page to share |
| `/v1/event.ics` | Downloadable `.ics` calendar file |
| `/v1/google` | Redirect to a pre-filled Google Calendar event |
| `/v1/outlook` | Redirect to a pre-filled Outlook event |

### Parameters

| Parameter | Required | Format and limit |
| --- | --- | --- |
| `title` | Yes | Up to 200 UTF-8 bytes; must not be blank after trimming |
| `start` | Yes | RFC 3339 timestamp with an explicit UTC offset |
| `end` | Yes | RFC 3339 timestamp with an explicit UTC offset; must be after `start` |
| `description` | No | Up to 4,000 UTF-8 bytes |
| `location` | No | Up to 500 UTF-8 bytes |
| `url` | No | Absolute HTTPS event URL, up to 2,048 UTF-8 bytes |

`start` and `end` may use `Z` or a numeric offset such as `+01:00`. URLendar
normalizes them to UTC and whole seconds. Events may last at most 366 days, and
the complete encoded query string may be at most 24 KiB. Unknown or duplicate
parameters return a `400` response with an `application/problem+json` body.

## Run it yourself

### Container image

Released images are available for `linux/amd64` and `linux/arm64`:

```sh
docker run --rm -p 8080:8080 \
  -e PUBLIC_ORIGIN=http://localhost:8080 \
  ghcr.io/stearz/urlendar:latest
```

Open <http://localhost:8080>. For a public deployment, set `PUBLIC_ORIGIN` to
the exact external origin, for example `https://calendar.example.com`.

The image runs as UID/GID `65532`, writes no files, and exposes `/healthz` for
health checks.

### From source

Go 1.27 or newer is required:

```sh
go test ./...
PUBLIC_ORIGIN=http://localhost:8080 go run .
```

Then open <http://localhost:8080>.

Configuration is supplied through environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `8080` | HTTP port the server listens on |
| `PUBLIC_ORIGIN` | `https://urlendar.stearz.net` | Public HTTP(S) origin used in canonical event links |

`PUBLIC_ORIGIN` must not contain credentials, a path, query, or fragment.

## Releases

`VERSION` contains the next stable semantic version. When a change to that file
is merged into `main`, the release workflow publishes a multi-architecture GHCR
image and creates the matching GitHub release (`v<version>`). Release versions
are immutable.

## License

URLendar is available under the [MIT License](LICENSE). You may use, modify, and
redistribute it, but it comes without warranty or liability.
