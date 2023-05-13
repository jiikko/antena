require 'fileutils'

class Post < ApplicationRecord

  EXECUTING_FLAG_PATH = '/tmp/.antena_post_destroying'
  ACCESSABLE_COLUMNS = %i(name site_id created_at url id)

  belongs_to :site, counter_cache: :posts_count

  has_many :word_taggings, dependent: :destroy
  has_many :word_tags, through: :word_taggings
  # has_many :posted_tweets, dependent: :destroy

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

  def self.destroy_old_posts
    loop do
      if File.exists?(PostWordTagger::EXECUTING_FLAG_PATH)
        sleep(40)
      else
        break
      end
    end

    ::FileUtils.touch(EXECUTING_FLAG_PATH)
    where('created_at < ?', Date.today.months_ago(3)).find_each(&:destroy)
    ::FileUtils.rm(EXECUTING_FLAG_PATH)
  end

  # moduleにするべきだな
  # modeule rss_readable
  def self.rss_to_post(site: nil)
    return nil if site.nil? or site.rss_url.nil?
    # フィード取得
    begin
      feed = Feedjira::Feed.fetch_and_parse(site.rss_url)
    rescue Faraday::TimeoutError
      Rails.logger.info "[Faraday::TimeoutError] #{site.inspect}"
      return
    rescue Timeout::Error
      Rails.logger.info "[Timeout::Error] #{site.inspect}"
      return
    rescue Feedjira::FetchFailure
      Rails.logger.info "[Feedjira::FetchFailure] #{site.inspect}"
      return
    rescue Faraday::ConnectionFailed
      Rails.logger.error "#{site.rss_url}でエラー(Faraday::ConnectionFailed)が起きました"
      return
    end

    return if feed.is_a?(Integer)
    # そのURLでDBに問い合わせ
    inserted_posts = Post.where("name in (?) or url in (?)", feed.entries.map(&:title), feed.entries.map(&:url))
    counter = 0
    feed.entries.each do |item|
      return if -> { DateTime.now }.call < item.published # 公開日が未来の場合は登録しない
      # URL かつ 記事名が既存になければ
      if !inserted_posts.detect { |x| x.url == item.url } && !inserted_posts.detect { |x| x.name == item.title }
        a = site.posts.build { |post|
          post.name = item.title
          post.url = item.url
          post.summary = item.summary
          post.content = item.content
          if site.rocketnews?
            post.content = ContentClearnupService.new(post).content
          end
          post.content = post.content_without_script_tag
          # 記事がfindで見つかったら登録しない。
        }
        if a.save
          # PostWordTagger.tagging!(a) デッドロックになる
          counter += 1
          # TwitterService.posted(a) if Rails.env.production?
        else
          Rails.logger.info "#{site.name}を読めませんでした"
        end
      end
    end
    counter
  end

  def self.search(word)
    # 早い順
    # Post.joins(:word_tags).where(word_tags: { name: word })
    # Post.joins(:word_tags).where(word_tags: { id: WordTag.where(name: "オリジナル") })
    # Post.joins(:word_tags).where(word_tags: { id: WordTag.where(name: word) })
    Post.where(
      id: WordTagging.where(
        word_tag_id: WordTag.where(
          name: word
        ).ids
      ).pluck(:post_id).uniq
    )
  end

  # 全サイトのrssを取得しにいく。treadつかう？
  def self.all_rss_to_post
    Site.enable.find_each do |site|
      if plus_number = Post.rss_to_post(site: site)
        p "#{site.name} sussecc!!(+#{plus_number})"
      else
        # エラーの原因は、RSS_URLが誤っているからかも
        p "[error] #{site.name}"
      end
    end
  end

  # NOTE 表示されることのない記事の削除を行う。3時くらいのcronで実行したい
  def self.destroy_for_old_posts
    posts = []

    Site.all.each do |site|
      if site.posts.size > 50
        posts << site.posts.where("created_at <= ?", DateTime.now - 5.day)
      end
    end

    count = posts.size
    posts.flatten.each{ |post| post.destroy }
    p "[notice] #{count}記事削除しました。"
  end

  def self.word_tagged(words)
    self.joins(:word_tags).where(word_tags: { name: [words] })
  end

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

  def only_image_tags_by_content
    if content_without_script_tag
      Nokogiri::HTML(content).css("img").map { |x|
        unless x.to_html.include?('width="1"')
          x.to_html
        end
      }.join.html_safe
    end
  end

  def word_tag_list=(names)
    if names.is_a?(String)
      names = names.split(",")
    end
    self.word_tags = names.map do |n|
      WordTag.where(name: n.strip).first_or_create!
    end
  end

  def reindex
    PostWordTagger.new(self).reindex
  end

  def clean_content!
    remove_kanren_text_in_content
    if site.rocketnews?
      self.content = ContentClearnupService.new(self).content
    end
    save!(validate: false)
  end
end
