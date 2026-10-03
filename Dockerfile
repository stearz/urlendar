FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.24 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -buildid=" -o /out/urlendar .

FROM scratch
COPY --from=build /out/urlendar /urlendar
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/urlendar"]
