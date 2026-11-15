# frozen_string_literal: true

# 本番DBから categories / sites を config/sites.yml の初版としてエクスポートする。
# あわせて posts の実測（件数・期間・容量）を STDERR に出す。
#
# 使い方（production 環境で実行すること）:
#   bin/rails runner bin/export_sites_yaml.rb > config/sites.yml
#
# - STDOUT には YAML だけを出す（リダイレクトでそのままファイルになる）
# - 実測レポートと警告は STDERR に出す
# - admin_users は含めない（パスワードハッシュをリポジトリに持ち込まないため）

unless defined?(Rails)
  abort "bin/rails runner 経由で実行してください"
end

warn "== export_sites_yaml (env: #{Rails.env}) =="

categories = Category.order(:id).to_a
sites = Site.order(:category_id, :position, :id).to_a
category_by_id = categories.index_by(&:id)

# --- 実測レポート ---
warn "categories: #{categories.size}"
warn "sites: #{sites.size} (enabled: #{sites.count(&:enable)})"
if Post.table_exists?
  oldest = Post.minimum(:created_at)
  newest = Post.maximum(:created_at)
  content_bytes = Post.sum(Arel.sql("COALESCE(LENGTH(content), 0) + COALESCE(LENGTH(summary), 0)"))
  per_site_max = Post.group(:site_id).count.values.max || 0
  warn "posts: #{Post.count} (per-site max: #{per_site_max})"
  warn "posts created_at: #{oldest} .. #{newest}"
  warn "posts content+summary bytes: #{content_bytes}"
end

# --- 警告（YAML化で欠けると困るもの） ---
sites.each do |site|
  warn "WARN: site id=#{site.id} の rss_url が空です" if site.rss_url.blank?
  warn "WARN: site id=#{site.id} の name が空です" if site.name.blank?
  warn "WARN: site id=#{site.id} の category (id=#{site.category_id}) が見つかりません" unless category_by_id[site.category_id]
end
categories.each do |category|
  warn "WARN: category slug=#{category.slug} に sites がありません" if sites.none? { |s| s.category_id == category.id }
end

# --- YAML 本体 ---
data = {
  "categories" => categories.map { |c|
    h = { "id" => c.id, "name" => c.name, "slug" => c.slug }
    h["identity"] = c.identity if c.has_attribute?(:identity) && c.identity.present?
    h
  },
  "sites" => sites.map { |s|
    h = {
      "id" => s.id,
      "name" => s.name,
      "url" => s.url,
      "rss_url" => s.rss_url,
    }
    h["description"] = s.description if s.description.present?
    h["category"] = category_by_id[s.category_id]&.slug
    h["position"] = s.position
    h["enable"] = s.enable
    h
  },
}

header = <<~HEADER
  # prpr-antena のカテゴリ／サイト定義（このファイルが正本）
  #
  # ルール:
  # - sites の id は固定。既存ページの URL (sites/:id) と対応しているので変更・再利用禁止。
  #   新規サイトは「現在の最大 id + 1」を採番する。
  # - サイトをやめるときは行を消さず enable: false にする。
  # - category は slug で参照する。categories の slug 変更も既存URLが壊れるので禁止。
  # - 並び順は position の昇順。
  # - このファイルを変更して master にマージすると、次回のバッチ実行から反映される。
HEADER

puts header
puts YAML.dump(data)
