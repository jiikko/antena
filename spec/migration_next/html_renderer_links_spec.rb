# frozen_string_literal: true

require "spec_helper"
require_relative "../../migration-next/lib/migration_next"

# Internal links must carry the page prefix exactly once: preview pages link
# inside /migration-preview/, public pages link to the live keys.
RSpec.describe MigrationNext::HtmlRenderer do
  let(:catalog) { MigrationNext::Catalog.load(File.read(File.expand_path("../../config/sites.yml", __dir__))) }
  let(:posts) do
    site = catalog.enabled_sites.first
    [MigrationNext::PostRecord.new("id" => 1, "site_id" => site.id, "title" => "t", "url" => "https://example.com/1",
                                   "created_at" => Time.now.utc.iso8601)]
  end

  def internal_hrefs(prefix)
    catalog.attach_posts(posts)
    pages = described_class.new(catalog: catalog, posts: posts, prefix: prefix).pages
    pages.values.flat_map { |html| html.scan(/href="(\/[^"]*)"/).flatten }
  end

  it "prefixes every internal link exactly once in preview pages" do
    hrefs = internal_hrefs("migration-preview")
    expect(hrefs).to include("/migration-preview/vip", "/migration-preview/index.html")
    expect(hrefs.reject { |href| href.start_with?("/migration-preview/") }).to eq([])
    expect(hrefs.grep(%r{migration-preview/migration-preview})).to eq([])
  end

  it "links to the live keys when the prefix is empty" do
    hrefs = internal_hrefs("")
    expect(hrefs).to include("/vip", "/index.html")
    expect(hrefs.grep(/migration-preview/)).to eq([])
  end
end
