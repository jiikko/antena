source "https://rubygems.org"
git_source(:github) { |repo| "https://github.com/#{repo}.git" }

ruby "3.2.0"

gem "rails"
gem "propshaft"
gem "mysql2"
gem "puma", "~> 5.0"
gem "importmap-rails"
gem "bootsnap", require: false
gem 'sorcery'
gem 'active_decorator'
gem 'feedjira'
gem 'acts_as_list'
gem 'meta-tags', :require => 'meta_tags'
gem 'kaminari'
gem 'json'
gem 'kgio'
gem 'nokogiri'
gem 'aws-sdk-s3'

group :development, :test do
  gem 'factory_bot_rails'
  gem 'rspec-rails'
  gem 'pry'
end

group :test do
  gem 'launchy'
  gem 'capybara'
end
