FROM node:24-bookworm-slim AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-bookworm AS core-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=web-build /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -o /out/agentmetry ./cmd/agentmetry

FROM alpine:3.23
RUN mkdir /data && chown 65532:65532 /data
COPY --from=core-build /out/agentmetry /agentmetry
USER 65532:65532
VOLUME ["/data"]
EXPOSE 17890 4317 4318
ENTRYPOINT ["/agentmetry"]
CMD ["-http-address", "0.0.0.0:17890", "-otlp-grpc-address", "0.0.0.0:4317", "-otlp-http-address", "0.0.0.0:4318", "-database", "/data/agentmetry.db"]
