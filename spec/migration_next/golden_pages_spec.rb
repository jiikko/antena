# frozen_string_literal: true

require "spec_helper"
require "json"
require "fileutils"
require_relative "../../migration-next/lib/migration_next"

# The rendered pages for a fixed input must not change unless the change is
# intended. These files are also the expected output for a reimplementation
# (spec/fixtures/golden/README.md). To accept a change: UPDATE_GOLDEN=1 bundle exec rspec
RSpec.describe "golden pages" do
  dir = File.expand_path("../fixtures/golden", __dir__)
  now = Time.iso8601("2026-11-27T12:00:00+09:00")

  def file_for(key) = key.end_with?(".html") ? key : "#{key.tr('/', '_')}.html"

  let(:pages) do
    catalog = MigrationNext::Catalog.load(File.read(File.join(dir, "input/sites.yml")))
    posts = MigrationNext::State.load(File.read(File.join(dir, "input/posts.json"))).posts
    catalog.attach_posts(posts)
    MigrationNext::HtmlRenderer.new(catalog: catalog, posts: posts, prefix: "", now: now).pages
  end
  let(:manifest_path) { File.join(dir, "pages/manifest.json") }

  before do
    next unless ENV["UPDATE_GOLDEN"] == "1"

    FileUtils.rm_rf(File.join(dir, "pages"))
    FileUtils.mkdir_p(File.join(dir, "pages"))
    manifest = pages.keys.sort.to_h { |key| [key, file_for(key)] }
    manifest.each { |key, file| File.write(File.join(dir, "pages", file), pages.fetch(key)) }
    File.write(manifest_path, JSON.pretty_generate(manifest) + "\n")
  end

  it "renders exactly the pages listed in the manifest" do
    manifest = JSON.parse(File.read(manifest_path))
    expect(pages.keys.sort).to eq(manifest.keys)
    expect(manifest.keys).to include("index.html", "vip", "news", "mom", "adult", "sites/1", "sites/1/3", "sites/10")
  end

  it "renders each page byte for byte as the golden file" do
    JSON.parse(File.read(manifest_path)).each do |key, file|
      expect(pages.fetch(key)).to eq(File.read(File.join(dir, "pages", file))), "#{key} differs from pages/#{file}"
    end
  end
end
