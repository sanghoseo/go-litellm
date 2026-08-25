FROM golang:1.27-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY litellm ./litellm
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/litellm-proxy ./cmd/litellm-proxy

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/litellm-proxy /usr/local/bin/litellm-proxy
COPY config.yaml /etc/litellm/config.yaml

EXPOSE 4000

ENTRYPOINT ["/usr/local/bin/litellm-proxy"]
CMD ["--config", "/etc/litellm/config.yaml", "--listen", ":4000"]
