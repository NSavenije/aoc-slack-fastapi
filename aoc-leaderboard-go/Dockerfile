# STACKIT Functions needs: linux/amd64, HTTP on $PORT, SIGTERM handling, numeric non-root user.
FROM --platform=linux/amd64 golang:1.24 AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /aoc .

FROM --platform=linux/amd64 gcr.io/distroless/static-debian12:nonroot
COPY --from=build /aoc /aoc
ENV PORT=8080
USER 65532
EXPOSE 8080
ENTRYPOINT ["/aoc"]
