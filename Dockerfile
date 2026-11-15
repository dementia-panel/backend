FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/backend ./cmd/backend

FROM alpine:3.23
RUN apk add --no-cache ca-certificates curl
COPY --from=build /out/backend /usr/local/bin/backend
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/backend"]
