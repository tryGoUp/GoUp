# Build stage
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=docker
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w -X github.com/mirkobrombin/goup/internal/cli.Version=${VERSION} -X github.com/mirkobrombin/goup/internal/cli.Commit=${COMMIT} -X github.com/mirkobrombin/goup/internal/cli.Date=${DATE}" \
    -o /out/goup cmd/goup/main.go

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/goup /usr/local/bin/goup
# Config in /etc/goup, logs/state in /var/lib/goup (XDG roots set to /etc and /var/lib).
ENV XDG_CONFIG_HOME=/etc
ENV XDG_DATA_HOME=/var/lib
VOLUME ["/etc/goup", "/var/lib/goup"]
ENTRYPOINT ["/usr/local/bin/goup"]
CMD ["start"]
