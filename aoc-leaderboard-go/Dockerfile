FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /aoc .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /aoc /aoc
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/aoc"]
