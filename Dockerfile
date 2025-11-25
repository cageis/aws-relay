FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o aws-relay .

FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /app/aws-relay .

EXPOSE 4567 4568

ENV AWS_UPSTREAM_URL=http://localstack:4566
ENV AWS_RELAY_ADDR=:4567
ENV AWS_DASHBOARD_ADDR=:4568

CMD ["./aws-relay"]
