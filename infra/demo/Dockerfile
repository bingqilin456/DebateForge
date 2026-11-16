FROM golang:1.27.1 AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/cmd ./cmd
COPY backend/internal ./internal
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -o /out/server ./cmd/server

FROM alpine:3.24.4
RUN mkdir -p /var/lib/siyige/demo && chown 10001:10001 /var/lib/siyige/demo && chmod 700 /var/lib/siyige/demo
COPY --from=build /out/server /usr/local/bin/server
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/server"]
