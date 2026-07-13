FROM docker.io/library/golang:1.26 as builder
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_TIME=unknown
WORKDIR /app/
COPY go.mod go.sum /app/
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -v \
    -ldflags "-s -w -X github.com/andatoshiki/omni/internal/version.Version=${VERSION} -X github.com/andatoshiki/omni/internal/version.Commit=${COMMIT} -X github.com/andatoshiki/omni/internal/version.BuildTime=${BUILD_TIME}" \
    -o omni

FROM alpine
COPY --from=builder /app/omni /app/omni

ENTRYPOINT ["/app/omni"]
ENV DS_API_KEY= BOT_TOKEN= CHAT_CMD= DS_INITIAL_PROMPT= DS_TEMPERATURE= DS_MAX_REPLY_TOKENS= DS_HISTORY_SIZE= ALLOWED_USERIDS= ADMIN_USERIDS= ALLOWED_GROUPIDS=
