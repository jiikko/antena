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
