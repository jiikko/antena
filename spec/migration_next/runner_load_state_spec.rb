# frozen_string_literal: true

require "spec_helper"
require "stringio"
require_relative "../../migration-next/lib/migration_next"

# The runner starts from an empty post set only when state/posts.json is
# absent; every other read failure must stop the run, because the run would
# otherwise overwrite the existing posts with the empty set.
RSpec.describe MigrationNext::Runner do
  let(:err) { StringIO.new }

  around do |example|
    previous = Aws.config[:s3]
    Aws.config[:s3] = { stub_responses: { get_object: get_object_stub } }
    example.run
  ensure
    Aws.config[:s3] = previous
  end

  def load_state
    described_class.new({}, log: StringIO.new, err: err).send(:load_state)
  end

  context "when state/posts.json does not exist" do
    let(:get_object_stub) { "NoSuchKey" }

    it "starts from an empty post set and warns" do
      expect(load_state.posts).to eq([])
      expect(err.string).to include("WARN migration-next: s3://prpr-antena/state/migration-preview/posts.json does not exist")
    end
  end

  context "when reading state/posts.json is denied" do
    let(:get_object_stub) { "AccessDenied" }

    it "raises instead of starting empty" do
      expect { load_state }.to raise_error(Aws::S3::Errors::AccessDenied)
    end
  end

  context "when state/posts.json is not valid JSON" do
    let(:get_object_stub) { { body: "{broken" } }

    it "raises instead of starting empty" do
      expect { load_state }.to raise_error(MigrationNext::Error, /posts state could not be read/)
    end
  end

  context "when a post has an empty created_at" do
    let(:get_object_stub) do
      { body: JSON.generate("posts" => [{ "id" => 7, "site_id" => 1, "created_at" => nil }]) }
    end

    it "raises as an unreadable state" do
      expect { load_state }.to raise_error(MigrationNext::Error, /post id=7 has no created_at/)
    end
  end

  context "when state/posts.json exists" do
    let(:get_object_stub) do
      { body: JSON.generate("posts" => [{ "id" => 7, "site_id" => 1, "title" => "t", "url" => "u",
                                          "created_at" => "2026-11-26T00:00:00Z" }]) }
    end

    it "loads the posts" do
      expect(load_state.posts.map(&:id)).to eq([7])
      expect(err.string).to be_empty
    end
  end
end
