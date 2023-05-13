#!/bin/bash

bundle exec rails db:migrate
bundle exec rails s -p ${PORT:-3000}
