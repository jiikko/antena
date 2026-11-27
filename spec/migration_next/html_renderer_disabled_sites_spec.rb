# frozen_string_literal: true

require "spec_helper"
require "nokogiri"
require_relative "../../migration-next/lib/migration_next"

# Disabled sites get no sites/<id> page, so no page may list or link to them.
RSpec.describe MigrationNext::HtmlRenderer do
  let(:catalog) do
    MigrationNext::Catalog.load(<<~YAML)
      categories:
      - {id: 1, name: VIP, slug: vip}
      sites:
      - {id: 1, name: live, url: "https://live.example/", rss_url: "https://live.example/rss", category: vip, position: 1, enable: true}
      - {id: 2, name: gone, url: "https://gone.example/", rss_url: "https://gone.example/rss", category: vip, position: 2, enable: false}
    YAML
  end
  let(:posts) do
    [1, 2].map do |site_id|
      MigrationNext::PostRecord.new("id" => site_id, "site_id" => site_id, "title" => "t#{site_id}",
                                    "url" => "https://example.com/#{site_id}", "created_at" => Time.now.utc.iso8601)
    end
  end
  let(:pages) do
    catalog.attach_posts(posts)
    described_class.new(catalog: catalog, posts: posts, prefix: "migration-preview").pages
  end

  it "links only to pages it generates" do
    expect(pages.keys).to include("sites/1")
    expect(pages.keys).not_to include("sites/2")
    pages.each do |key, html|
      targets = Nokogiri::HTML(html).css("a[href^='/migration-preview/']").map { |a| a["href"].delete_prefix("/migration-preview/") }
      expect(targets).to include("sites/1") if %w[index.html vip].include?(key)
      expect(targets - pages.keys).to eq([]), "#{key} links to pages that are not generated"
    end
  end
end
