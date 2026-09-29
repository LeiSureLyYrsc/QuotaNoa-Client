# syntax=docker/dockerfile:1

FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/version.Version=${VERSION}" \
    -o /out/quotanoa-client ./cmd/quotanoa-client

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/quotanoa-client /usr/local/bin/quotanoa-client
COPY --from=build /src/config.example.json /config.example.json
ENTRYPOINT ["/usr/local/bin/quotanoa-client"]
CMD ["run", "--config", "/config/config.json"]
