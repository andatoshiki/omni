FROM docker.io/library/golang:1.26 as builder
WORKDIR /app/
COPY go.mod go.sum /app/
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -v -o omni

FROM alpine
COPY --from=builder /app/omni /app/omni

ENTRYPOINT ["/app/omni"]
ENV DS_API_KEY= BOT_TOKEN= CHAT_CMD= DS_INITIAL_PROMPT= DS_TEMPERATURE= DS_MAX_REPLY_TOKENS= DS_HISTORY_SIZE= ALLOWED_USERIDS= ADMIN_USERIDS= ALLOWED_GROUPIDS=
