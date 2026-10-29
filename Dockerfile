FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /laukpauk ./cmd/laukpauk

FROM gcr.io/distroless/static-debian12
COPY --from=build /laukpauk /laukpauk
WORKDIR /app
VOLUME /app/data
EXPOSE 5000
ENTRYPOINT ["/laukpauk"]
