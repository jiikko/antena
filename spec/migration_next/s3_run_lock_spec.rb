# frozen_string_literal: true

require "spec_helper"
require "stringio"
require_relative "../../migration-next/lib/migration_next"

# Exercises the lock against an in-memory S3 that honours the conditional
# headers the lock adds by hand (If-None-Match / If-Match), so the takeover
# rules are checked through the real request path rather than a mocked lock.
RSpec.describe MigrationNext::S3RunLock do
  let(:objects) { {} }
  let(:client) do
    Aws::S3::Client.new(stub_responses: true, region: "ap-northeast-1").tap do |c|
      c.stub_responses(:put_object, lambda { |ctx|
        headers = ctx.http_request.headers
        current = objects[ctx.params[:key]]
        next({ status_code: 412, headers: {}, body: "" }) if headers["If-None-Match"] == "*" && current
        if (expected = headers["If-Match"])
          # Real S3 answers If-Match on a missing key with 404 NoSuchKey.
          next({ status_code: 404, headers: {}, body: "" }) if current.nil?
          next({ status_code: 412, headers: {}, body: "" }) if current[:etag] != expected
        end

        body = ctx.params[:body]
        body = body.read if body.respond_to?(:read)
        etag = %("#{SecureRandom.hex(8)}")
        objects[ctx.params[:key]] = { body: body, etag: etag }
        { etag: etag }
      })
      c.stub_responses(:get_object, lambda { |ctx|
        current = objects[ctx.params[:key]]
        current ? { body: current[:body], etag: current[:etag] } : "NoSuchKey"
      })
      c.stub_responses(:delete_object, lambda { |ctx|
        objects.delete(ctx.params[:key])
        {}
      })
    end
  end
  let(:store) { Struct.new(:client, :bucket, :lock_key).new(client, "page-bucket", "locks/shadow/migration-next-run.lock") }
  let(:t0) { Time.utc(2026, 11, 26, 0, 0, 0) }
  let(:err) { StringIO.new }

  def lock_at(time, stale_after: 3600)
    described_class.new(store, err: err, stale_after: stale_after, clock: -> { time })
  end

  def lock_owner_started_at
    JSON.parse(objects.fetch(store.lock_key)[:body])["started_at"]
  end

  it "skips with a warning while a fresh lock is held" do
    expect(lock_at(t0).acquire).to be true
    expect(lock_at(t0 + 60).acquire).to be false
    expect(err.string).to include("WARN migration-next skipped")
  end

  it "takes over a lock older than stale_after and warns" do
    expect(lock_at(t0).acquire).to be true
    expect(lock_at(t0 + 3601).acquire).to be true
    expect(lock_owner_started_at).to eq((t0 + 3601).iso8601(6))
    expect(err.string).to include("WARN migration-next took over a stale S3 lock")
  end

  it "does not let a second waiter take over with an outdated etag" do
    expect(lock_at(t0).acquire).to be true
    stale_body, stale_etag = objects.fetch(store.lock_key).values_at(:body, :etag)

    expect(lock_at(t0 + 3601).acquire).to be true
    late = lock_at(t0 + 3602)
    expect(late.send(:take_over, JSON.parse(stale_body), stale_etag)).to be false
    expect(lock_owner_started_at).to eq((t0 + 3601).iso8601(6))
  end

  it "does not delete a lock that another execution took over" do
    original = lock_at(t0)
    expect(original.acquire).to be true
    expect(lock_at(t0 + 3601).acquire).to be true

    original.release
    expect(objects).to have_key(store.lock_key)
    expect(lock_owner_started_at).to eq((t0 + 3601).iso8601(6))
  end

  it "treats a lock with an unreadable started_at as stale" do
    objects[store.lock_key] = { body: JSON.generate("token" => "x", "started_at" => "garbage"), etag: %("e1") }
    expect(lock_at(t0).acquire).to be true
    expect(err.string).to include("unreadable started_at")
  end

  it "takes over a lock whose body is not a JSON object" do
    objects[store.lock_key] = { body: "", etag: %("e1") }
    expect(lock_at(t0).acquire).to be true
    expect(err.string).to include("not a JSON object")
  end

  it "gives up a takeover when the stale lock disappeared before the If-Match PUT" do
    late = lock_at(t0 + 3601)
    expect(late.send(:take_over, { "started_at" => t0.iso8601 }, %("gone"))).to be false
    expect(objects).not_to have_key(store.lock_key)
  end

  it "releases its own lock" do
    lock = lock_at(t0)
    expect(lock.acquire).to be true
    lock.release
    expect(objects).not_to have_key(store.lock_key)
  end
end
