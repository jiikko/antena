# syntax=docker/dockerfile:1
FROM ruby:3.2.2 as base

WORKDIR /app

COPY Gemfile /app/Gemfile
COPY Gemfile.lock /app/Gemfile.lock

RUN bundle config set --local path '/vendor/bundle'


# target dev
FROM base as dev
LABEL image.type=dev
RUN bundle install --jobs 100


# target production
FROM base as production
LABEL image.type=production
ENV RAILS_ENV production

COPY . .
RUN bundle install --jobs 10 --without development,test
COPY . /app
# assetsがないのでskip
# RUN SECRET_KEY_BASE=dummy bundle exec rails assets:precompile

CMD ["bin/start.sh"]
