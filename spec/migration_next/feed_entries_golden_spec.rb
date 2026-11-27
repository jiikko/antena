# frozen_string_literal: true

require "spec_helper"
require "json"
require "time"
require_relative "../../migration-next/lib/migration_next"

# Real RSS bodies recorded on 2026-11-27 and the entries the Ruby implementation
# makes from them (FeedEntries.parse): the expected output for a reimplementation.
# They hold third-party article content, so they are kept out of this public
# repository (user decision 2026-11-27): only on the machine that recorded them,
# under tmp/ (or ANTENA_FEED_FIXTURES). Without them these examples are pending.
# Files per site: <id>.xml (body), <id>.meta.json (url, status, recorded_at),
# <id>.expected.json. To accept a change: UPDATE_GOLDEN=1 bundle exec rspec <this file>
RSpec.describe MigrationNext::FeedEntries do
  dir = ENV.fetch("ANTENA_FEED_FIXTURES", File.expand_path("../../tmp/feed-record-20261127", __dir__))
  metas = Dir[File.join(dir, "*.meta.json")].sort

  def entries_json(entries)
    entries.map { |entry| entry.merge("published_at" => entry["published_at"]&.utc&.iso8601(6)) }
  end

  if metas.empty?
    it("matches the recorded feeds") { skip "no recorded feeds in #{dir}" }
  end

  metas.each do |meta_path|
    meta = JSON.parse(File.read(meta_path))
    id = meta.fetch("site_id")

    it "turns the recorded feed of site #{id} into the expected entries" do
      body = File.binread(File.join(dir, "#{id}.xml"))
      actual = entries_json(described_class.parse(body, now: Time.iso8601(meta.fetch("recorded_at"))))
      expected_path = File.join(dir, "#{id}.expected.json")
      File.write(expected_path, JSON.pretty_generate(actual) + "\n") if ENV["UPDATE_GOLDEN"] == "1"

      expect(actual).not_to be_empty
      expect(actual).to eq(JSON.parse(File.read(expected_path)))
    end
  end
end
