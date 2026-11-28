# syntax=docker/dockerfile:1
# The batch that writes https://prpr-antena.com/ (src/, the Go implementation).
# release.yml builds the go target and deploys it to the Cloud Run job
# antena-fetch-posts-next with the same command.
FROM golang:1.26.0 AS go-build
WORKDIR /src
COPY src/go.mod src/go.sum ./
RUN go mod download
COPY src/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /antena-batch ./cmd/antena-batch

# target go
FROM gcr.io/distroless/static-debian12 AS go
WORKDIR /app
COPY config/sites.yml /app/config/sites.yml
COPY --from=go-build /antena-batch /app/antena-batch
CMD ["/app/antena-batch"]
