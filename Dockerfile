FROM golang:1.18-alpine AS build

WORKDIR /app

COPY . .

RUN go build -o scrbble cmd/server/main.go

FROM alpine:3

COPY --from=build /app/scrbble /app/scrbble

CMD /app/scrbble