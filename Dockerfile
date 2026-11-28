# syntax=docker/dockerfile:1
FROM ruby:3.2.2 AS base

WORKDIR /app

COPY Gemfile /app/Gemfile
COPY Gemfile.lock /app/Gemfile.lock

RUN bundle config set --local path '/vendor/bundle'


# target dev
FROM base AS dev
LABEL image.type=dev
RUN bundle install --jobs 100


# target production
FROM base AS production
LABEL image.type=production

COPY . .
RUN bundle config set --local without 'development test' && bundle install --jobs 10

# The Cloud Run job antena-fetch-posts-next sets the same command (release.yml).
CMD ["bundle", "exec", "ruby", "migration-next/bin/run.rb"]


# The Go port of the batch (batch/). Built as its own target; release.yml
# pushes it as a separate image tag.
FROM golang:1.26.0 AS go-build
WORKDIR /src
COPY batch/go.mod batch/go.sum ./
RUN go mod download
COPY batch/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /antena-batch ./cmd/antena-batch

# target go
FROM gcr.io/distroless/static-debian12 AS go
WORKDIR /app
COPY config/sites.yml /app/config/sites.yml
COPY --from=go-build /antena-batch /app/antena-batch
CMD ["/app/antena-batch"]
