FROM golang:1.27.1-trixie AS go-build
ENV CGO_ENABLED=0 GOTOOLCHAIN=local

FROM go-build AS app-build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/cmd/server ./cmd/server
COPY backend/internal ./internal
RUN go build -mod=readonly -trimpath -o /out/server ./cmd/server

FROM go-build AS migrate-build
WORKDIR /src
COPY backend/tools/migrate/ ./
RUN go mod download && go build -mod=readonly -trimpath -o /out/migrate .

FROM alpine:3.24.4 AS go-runtime
USER 10001:10001
WORKDIR /opt/siyige

FROM go-runtime AS app
COPY --from=app-build /out/server /usr/local/bin/server
ENTRYPOINT ["/usr/local/bin/server"]

FROM go-runtime AS migrate
COPY --from=migrate-build /out/migrate /usr/local/bin/migrate
COPY backend/migrations/*.sql /opt/siyige/migrations/
ENTRYPOINT ["/usr/local/bin/migrate", "-env", "deploy", "-password-file", "/run/secrets/migrator_password", "-dir", "/opt/siyige/migrations"]

FROM node:24.11.1-bookworm-slim AS web-build
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM nginx:1.30.6-alpine-slim AS edge
COPY --from=web-build /src/dist/ /usr/share/nginx/html/
COPY deploy/nginx/nginx.conf /etc/nginx/nginx.conf
COPY deploy/nginx/site.conf.template deploy/nginx/proxy.conf deploy/nginx/proxy-sse.conf /etc/nginx/siyige/
COPY deploy/nginx/entrypoint.sh /opt/siyige/entrypoint.sh
USER 101:101
ENTRYPOINT ["/bin/sh", "/opt/siyige/entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
