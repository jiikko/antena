require 'fileutils'

class Post < ApplicationRecord

  ACCESSABLE_COLUMNS = %i(name site_id created_at url id)

  belongs_to :site, counter_cache: :posts_count

  scope :saikin_post, -> { where("posts.created_at >= ?", DateTime.now - 2.day).order("id DESC") }
  scope :enable_site, -> { joins(:site).where("sites.enable = true") }
  # scope :without, ->(post){ where.not(id: post) }
  scope :yet_tagged, ->{ where(tagged: false) }
  scope :yet_tweet, ->(tag) {
    where.not(
      id: PostedTweet.select(:post_id)
    ).limit(1)
  }

  validates :site_id, presence: true
  validates :url, presence: true, :uniqueness => true
  validates :name, uniqueness: { scope: :site_id }
  # validates :url, presence: true, :uniqueness => { :scope => [:url, :name] }

  before_save :remove_kanren_text_in_content

  # http://ngyuki.hatenablog.com/entry/20120724/p1
  def self.posts_by(sites: )
    sqls =
      sites.map { |site|
        Post.where("site_id = #{site.id}").
          select(:id, :site_id, :name, :url).
          order(created_at: :desc).limit(4).to_sql
    }
    union_sql = "(#{sqls.join(' ) union all ( ')})"
    self.find_by_sql(union_sql)
  end

  def content_without_script_tag
    if content
      x = content.gsub(%r!<script[^>]*>.*</script>!m, '')
      x = x.gsub(%r!<img src="[^"]*" width=.1. height=.1[^>]*>!m, '')
      x = x.gsub(%r_<!--[^-]*-->m_, '')
      x = x.gsub(%r!<a href=.http://2ch-c.net/[^>]*>[^<]*</a>!m, '')
      x = x.gsub(%r!<br( /)?>!, '')
      x = x.gsub(%r!<a [^>]*>続きを読む</a>!m, '')
      x = x.gsub(%r!&nbsp;!, '')
      x = x.gsub(%r!&gt;!, '')
    end
  end

  def summary_without_script_tag
    if summary
      x = summary.gsub(%r!<script[^>]*>.*</script>!m, '')
      x = x.gsub(%r!<img src="[^"]*" width=.1. height=.1[^>]*>!m, '')
      x = x.gsub(%r_<!--[^-]*-->m_, '')
      x = x.gsub(%r!<a href=.http://2ch-c.net/[^>]*>[^<]*</a>!m, '')
      x = x.gsub(%r!<br( /)?>!, '')
      x = x.gsub(%r!<a [^>]*>続きを読む</a>!m, '')
      x = x.gsub(%r!&nbsp;!, '')
      x = x.gsub(%r!&gt;!, '')
    end
  end

  def name_with_short
    if name
      name.gsub('ｗｗｗｗ', '')
    end
  end

  def remove_kanren_text_in_content
    doc = Nokogiri::HTML.parse(content || summary)
    doc.css("[id=related-title]").remove
    doc.css(".automatic-related").remove

    # アルファモザク
    doc.css("[id=article_mid]").remove
    doc.css(".kijinai").remove
    self.content = doc.to_html
  end

  def clean_content!
    remove_kanren_text_in_content
    if site.rocketnews?
      self.content = ContentClearnupService.new(self).content
    end
    save!(validate: false)
  end

  def only_image_tags_by_content
    if content_without_script_tag
      Nokogiri::HTML(content).css("img").map { |x|
        unless x.to_html.include?('width="1"')
          x.to_html
        end
      }.join.html_safe
    end
  end

end
