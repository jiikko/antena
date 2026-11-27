# frozen_string_literal: true

require "spec_helper"
require "nokogiri"
require_relative "../../migration-next/lib/migration_next"

# The old Rails pages show the heading for the all-category feed only above the
# right-column sidebar (app/views/shared/_right_column.html.erb). The category
# menu (app/views/shared/_header.html.erb) has no heading of its own.
RSpec.describe MigrationNext::HtmlRenderer do
  let(:catalog) { MigrationNext::Catalog.load(File.read(File.expand_path("../../config/sites.yml", __dir__))) }
  let(:posts) do
    site = catalog.enabled_sites.first
    [MigrationNext::PostRecord.new("id" => 1, "site_id" => site.id, "title" => "t", "url" => "https://example.com/1",
                                   "created_at" => Time.now.utc.iso8601)]
  end
  let(:pages) do
    catalog.attach_posts(posts)
    described_class.new(catalog: catalog, posts: posts, prefix: "migration-preview").pages
  end

  it "shows the all-category heading only in the right column of every page" do
    expect(pages.keys).to include(a_string_matching(%r{sites/}), a_string_matching(/vip/))
    pages.each do |key, html|
      doc = Nokogiri::HTML(html)
      headings = doc.css("h3").select { |h| h.text.strip == "全カテゴリの新着記事" }
      expect(headings.size).to eq(1), "#{key}: #{headings.size} headings"
      expect(headings.first.ancestors(".right_column")).not_to be_empty, "#{key}: heading outside .right_column"
    end
  end
end
