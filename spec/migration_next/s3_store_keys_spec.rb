# frozen_string_literal: true

require "spec_helper"
require_relative "../../migration-next/lib/migration_next"

# The live run keeps state/posts.json and its lock where they have always been.
# A run under a page prefix (a shadow run of another implementation) must get
# its own state and lock, still below state/ and locks/ so the bucket policy
# Deny on those prefixes keeps them unreadable from the site.
RSpec.describe MigrationNext::S3Store do
  around do |example|
    previous = Aws.config[:s3]
    Aws.config[:s3] = { stub_responses: true }
    example.run
  ensure
    Aws.config[:s3] = previous
  end

  context "with the live (empty) prefix" do
    subject(:store) { described_class.new("MIGRATION_NEXT_PREFIX" => "", "MIGRATION_NEXT_ALLOW_PUBLIC" => "true") }

    it "keeps the live state, lock and page keys" do
      expect(store.state_key).to eq("state/posts.json")
      expect(store.lock_key).to eq("locks/migration-next-run.lock")
      expect(store.key("index.html")).to eq("index.html")
    end
  end

  context "with a page prefix" do
    subject(:store) { described_class.new("MIGRATION_NEXT_PREFIX" => "/go-preview/") }

    it "separates the state and the lock from the live run" do
      expect(store.state_key).to eq("state/go-preview/posts.json")
      expect(store.lock_key).to eq("locks/go-preview/migration-next-run.lock")
      expect(store.key("index.html")).to eq("go-preview/index.html")
    end
  end

  context "without MIGRATION_NEXT_PREFIX" do
    subject(:store) { described_class.new({}) }

    it "uses the migration-preview state, not the live one" do
      expect(store.state_key).to eq("state/migration-preview/posts.json")
      expect(store.lock_key).to eq("locks/migration-preview/migration-next-run.lock")
    end
  end
end
