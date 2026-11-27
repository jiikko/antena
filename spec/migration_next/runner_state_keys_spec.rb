# frozen_string_literal: true

require "spec_helper"
require "stringio"
require "tempfile"
require_relative "../../migration-next/lib/migration_next"

# Runs the whole batch against an in-memory S3 and records which keys it
# touches. The state holds full feed bodies: it must be read and written below
# state/ (hidden from the site by the bucket policy), and a run under a page
# prefix must not touch the live state or lock.
RSpec.describe MigrationNext::Runner do
  let(:objects) { {} }
  let(:writes) { [] }
  let(:sites_file) do
    Tempfile.new(["sites", ".yml"]).tap do |file|
      file.write(<<~YAML)
        categories:
        - {id: 1, name: VIP, slug: vip}
        sites:
        - {id: 1, name: site, url: "https://example.com/", rss_url: "https://example.com/rss", category: vip, position: 1, enable: false}
      YAML
      file.flush
    end
  end

  around do |example|
    previous = Aws.config[:s3]
    Aws.config[:s3] = {
      stub_responses: {
        put_object: lambda { |ctx|
          body = ctx.params[:body]
          body = body.read if body.respond_to?(:read)
          writes << ctx.params[:key]
          objects[ctx.params[:key]] = body
          { etag: %("#{writes.size}") }
        },
        get_object: lambda { |ctx|
          objects.key?(ctx.params[:key]) ? { body: objects[ctx.params[:key]], etag: %("e") } : "NoSuchKey"
        },
        delete_object: lambda { |ctx|
          objects.delete(ctx.params[:key])
          {}
        },
      },
    }
    example.run
  ensure
    Aws.config[:s3] = previous
  end

  def run_batch(env)
    described_class.new(env.merge("MIGRATION_NEXT_SITES_FILE" => sites_file.path), log: StringIO.new, err: StringIO.new).execute
  end

  it "keeps the live state and lock keys with the empty prefix" do
    run_batch("MIGRATION_NEXT_PREFIX" => "", "MIGRATION_NEXT_ALLOW_PUBLIC" => "true")

    expect(writes).to include("locks/migration-next-run.lock", "state/posts.json", "index.html")
    expect(writes.grep(/posts\.json/)).to eq(["state/posts.json"])
    expect(objects.keys).not_to include("locks/migration-next-run.lock")
  end

  it "reads and writes only its own state and lock under a page prefix" do
    objects["state/posts.json"] = "live"
    run_batch("MIGRATION_NEXT_PREFIX" => "go-preview")

    expect(writes).to include("locks/go-preview/migration-next-run.lock", "state/go-preview/posts.json", "go-preview/index.html")
    expect(writes.grep(/posts\.json|\.lock/)).to eq(["locks/go-preview/migration-next-run.lock", "state/go-preview/posts.json"])
    expect(objects["state/posts.json"]).to eq("live")
  end
end
