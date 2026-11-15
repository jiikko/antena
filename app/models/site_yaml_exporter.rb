# frozen_string_literal: true

require "yaml"

# RDB切り離し移行用の一時コード。
# DB上の categories / sites を YAML 化して S3 に置く（config/sites.yml の初版の元データ）。
# 移行が終わったら Cron::FetchAndUpload からの呼び出しごと削除する。
class SiteYamlExporter
  OBJECT_KEY = "migration/sites.yml"

  HEADER = <<~HEADER
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

  def initialize
    @s3 = Aws::S3::Resource.new(region: "ap-northeast-1")
    @bucket_name = Rails.configuration.x.s3_bucket_name
  end

  def execute
    @s3.bucket(@bucket_name).object(OBJECT_KEY).put(
      body: self.class.build_yaml,
      content_type: "text/yaml; charset=utf-8",
    )
    Rails.logger.info "[SiteYamlExporter] uploaded s3://#{@bucket_name}/#{OBJECT_KEY}"
    log_stats
  end

  def self.build_yaml
    categories = Category.order(:id).to_a
    sites = Site.order(:category_id, :position, :id).to_a
    category_by_id = categories.index_by(&:id)

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

    HEADER + YAML.dump(data)
  end

  private

  def log_stats
    per_site_max = Post.group(:site_id).count.values.max || 0
    content_bytes = Post.sum(Arel.sql("COALESCE(LENGTH(content), 0) + COALESCE(LENGTH(summary), 0)"))
    Rails.logger.info "[SiteYamlExporter] categories=#{Category.count} sites=#{Site.count} " \
      "posts=#{Post.count} per_site_max=#{per_site_max} " \
      "posts_period=#{Post.minimum(:created_at)}..#{Post.maximum(:created_at)} " \
      "content_bytes=#{content_bytes}"

    Site.find_each do |site|
      Rails.logger.warn "[SiteYamlExporter] site id=#{site.id} rss_url is blank" if site.rss_url.blank?
      Rails.logger.warn "[SiteYamlExporter] site id=#{site.id} name is blank" if site.name.blank?
    end
  end
end
