class Site < ApplicationRecord
  before_create :site_to_rss_url
  after_create :set_site_meta

  acts_as_list scope: :category

  scope :enable, -> { where(enable: true) }

  has_many :posts, -> {  order("id DESC") }, dependent: :destroy
  has_many :saikin_posts_for_5, -> { order("id DESC").limit(5) }, class_name: 'Post'
  has_many :saikin_posts_for_50, -> { order("id DESC").limit(300) }, class_name: 'Post'

  belongs_to :category

  #validates :name, uniqueness: true
  validates :url, presence: true
  validates :category_id, presence: true

  # @return [void]
  def self.fetch
    Site.enable.find_each do |site|
      begin
        if(plus_number = site.fetch)
          Rails.logger.info "#{site.name} sussecc!!(+#{plus_number})"
        else
          # エラーの原因は、RSS_URLが誤っているからかも
          Rails.logger.error "[error] #{site.name}"
        end
      rescue => e
        Rails.logger.error e
      end
    end
  end

  # @return [Integer]
  def fetch
    return nil if rss_url.nil?

    # フィード取得
    begin
      response = Net::HTTP.get_response(URI(rss_url))
      feed = Feedjira.parse(response.body)
    rescue Faraday::TimeoutError
      Rails.logger.info "[Faraday::TimeoutError] #{inspect}"
      return
    rescue Timeout::Error
      Rails.logger.info "[Timeout::Error] #{inspect}"
      return
    rescue Feedjira::FetchFailure
      Rails.logger.info "[Feedjira::FetchFailure] #{inspect}"
      return
    rescue Faraday::ConnectionFailed
      Rails.logger.error "#{rss_url}でエラー(Faraday::ConnectionFailed)が起きました"
      return
    end

    return if feed.is_a?(Integer)
    inserted_posts = Post.where("name in (?) or url in (?)", feed.entries.map(&:title), feed.entries.map(&:url))

    counter = 0
    feed.entries.each do |item|
      return if -> { DateTime.now }.call < item.published # 公開日が未来の場合は登録しない
      # URL かつ 記事名が既存になければ
      # FIXME: hashを使って捜査をする
      if !inserted_posts.detect { |x| x.url == item.url } && !inserted_posts.detect { |x| x.name == item.title }
        post = posts.build { |post|
          post.name = item.title
          post.url = item.url
          post.summary = item.summary
          post.content = item.content
          post.content = post.content_without_script_tag
        }
        if post.save
          counter += 1
        else
          Rails.logger.info "#{name}を読めませんでした"
        end
      end
    end
    counter
  end

  # NOTE 表示されることのない記事の削除を行う。3時くらいのcronで実行したい
  def self.destroy_old_posts
    posts = []

    Site.all.each do |site|
      if site.posts.size > 50
        posts << site.posts.where("created_at <= ?", DateTime.now - 5.day)
      end
    end

    count = posts.size
    posts.flatten.each{ |post| post.destroy }
  end

  # SiteURLからRSS_URLを取得してくる
  def site_to_rss_url
    return if self.rss_url.present?

    response = Net::HTTP.get_response(URI(url))
    doc = nil
    begin
      doc = Nokogiri::HTML(response.body) 
    rescue => e
      Rails.logger.error e
      self.update!(enable: false)
      return
    end

    doc.css("link").each do |ele|
      if ele['type'] =~ %r!application/[\w]*?\+xml!
        self.rss_url = ele['href']
        break
      end
    end
  end

  # サイト名を取得
  def set_site_meta
    if self.rss_url
      response = Net::HTTP.get_response(URI(rss_url))
      feed = Feedjira.parse(response.body)
      self.name = feed.title
      self.save
    else
      p "[error] サイト名とRSS_URLを取得できませんでした。at #{self.url}"
    end
  rescue => e
    Rails.logger.error e
    self.update!(enable: false)
  end
end

