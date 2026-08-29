FROM golang:1.26 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /mockreceiver ./test/e2e/mockreceiver

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /mockreceiver /mockreceiver
EXPOSE 9099
ENTRYPOINT ["/mockreceiver"]
