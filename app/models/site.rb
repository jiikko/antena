require 'open-uri'

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

  # SiteURLからRSS_URLを取得してくる
  def site_to_rss_url
    return if self.rss_url.present?

    doc = Nokogiri::HTML(open(self.url)) rescue (@destroy_flag = true; return)
    if doc.css("div").blank?
      doc = Nokogiri::XML(open(self.url))
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
    if @destroy_flag
      destroy
      return
    end

    if self.rss_url
      feed = Feedjira::Feed.fetch_and_parse(self.rss_url)
      self.name = feed.title
      self.url = feed.url
      self.save
    else
      p "[error] サイト名とRSS_URLを取得できませんでした。at #{self.url}"
    end
  end

  def rocketnews?
    name == 'ロケットニュース24'
  end
end

