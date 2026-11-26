# frozen_string_literal: true

require "aws-sdk-s3"
require "cgi"
require "date"
require "feedjira"
require "json"
require "net/http"
require "nokogiri"
require "securerandom"
require "set"
require "time"
require "yaml"

module MigrationNext
  class Error < StandardError; end

  class Category
    attr_reader :id, :name, :slug, :identity, :sites

    def initialize(data)
      @id = Integer(data.fetch("id"))
      @name = data.fetch("name").to_s
      @slug = data.fetch("slug").to_s
      @identity = data["identity"]
      @sites = []
    end
  end

  class Site
    attr_reader :id, :name, :url, :rss_url, :description, :category, :position, :posts

    def initialize(data, category)
      @id = Integer(data.fetch("id"))
      @name = data.fetch("name").to_s
      @url = data["url"].to_s
      @rss_url = data["rss_url"].to_s
      @description = data["description"].to_s
      @category = category
      @position = Integer(data["position"] || 0)
      @enabled = data["enable"] != false
      @posts = []
    end

    def enabled? = @enabled
    def posts_count = posts.size
    def page_path(page = 1) = page <= 1 ? "/sites/#{id}" : "/sites/#{id}/#{page}"
  end

  class PostRecord
    attr_reader :id, :site_id, :title, :url, :summary, :content, :published_at,
                :created_at, :updated_at
    attr_accessor :site

    def initialize(data)
      @id = Integer(data.fetch("id"))
      @site_id = Integer(data.fetch("site_id"))
      @title = data.key?("title") ? data["title"] : data["name"]
      @url = data["url"]
      @summary = data["summary"]
      @content = data["content"]
      @published_at = parse_time(data["published_at"])
      @created_at = parse_time(data.fetch("created_at"))
      @updated_at = parse_time(data["updated_at"])
    end

    def name = title

    def to_h
      {
        "id" => id,
        "site_id" => site_id,
        "title" => title,
        "url" => url,
        "summary" => summary,
        "content" => content,
        "published_at" => published_at&.iso8601(6),
        "created_at" => created_at.iso8601(6),
        "updated_at" => updated_at&.iso8601(6),
      }
    end

    def name_with_short = title&.gsub("ｗｗｗｗ", "")

    def only_image_tags_by_content
      return "" unless content

      Nokogiri::HTML(content).css("img").filter_map do |image|
        image.to_html unless image.to_html.include?('width="1"')
      end.join
    end

    private

    def parse_time(value)
      return nil if value.nil? || value == ""
      return value if value.is_a?(Time)

      Time.iso8601(value.to_s)
    rescue ArgumentError
      Time.parse(value.to_s)
    end
  end

  class Catalog
    attr_reader :categories, :sites

    def self.load(yaml_text)
      new(YAML.safe_load(yaml_text, permitted_classes: [Date, DateTime, Time], aliases: true) || {})
    rescue Psych::Exception => e
      raise Error, "sites YAML could not be read: #{e.message}"
    end

    def initialize(data)
      categories_data = data.fetch("categories") { raise Error, "sites YAML has no categories" }
      sites_data = data.fetch("sites") { raise Error, "sites YAML has no sites" }
      @categories = categories_data.map { |row| Category.new(row) }
      @categories_by_slug = unique_index(@categories, &:slug)
      @categories_by_id = unique_index(@categories, &:id)
      @sites = sites_data.map do |row|
        category = @categories_by_slug[row.fetch("category").to_s]
        raise Error, "site id=#{row['id']} references unknown category #{row['category'].inspect}" unless category

        Site.new(row, category).tap { |site| category.sites << site }
      end
      @sites_by_id = unique_index(@sites, &:id)
      @categories.each { |category| category.sites.sort_by! { |site| [site.position, site.id] } }
    rescue KeyError, ArgumentError, TypeError => e
      raise Error, "invalid sites YAML: #{e.message}"
    end

    def category_by_slug(slug) = @categories_by_slug[slug]
    def site_by_id(id) = @sites_by_id[Integer(id)]
    def enabled_sites = sites.select(&:enabled?)

    def attach_posts(posts)
      sites_by_id = sites.to_h { |site| [site.id, site] }
      sites.each { |site| site.posts.clear }
      posts.each do |post|
        site = sites_by_id[post.site_id]
        raise Error, "post id=#{post.id} references unknown site id=#{post.site_id}" unless site

        post.site = site
        site.posts << post
      end
    end

    private

    def unique_index(items)
      index = items.to_h { |item| [yield(item), item] }
      raise Error, "duplicate id or slug in sites YAML" unless index.size == items.size

      index
    end
  end

  class State
    attr_reader :posts

    def self.load(json_text)
      data = JSON.parse(json_text)
      rows = data.is_a?(Array) ? data : data.fetch("posts")
      new(rows.map { |row| PostRecord.new(row) })
    rescue JSON::ParserError, KeyError, ArgumentError, TypeError => e
      raise Error, "posts state could not be read: #{e.message}"
    end

    def initialize(posts)
      @posts = posts
    end

    def next_id = (posts.map(&:id).max || 0) + 1

    def to_json
      JSON.generate(
        "schema_version" => 1,
        "exported_at" => Time.now.utc.iso8601(6),
        "posts" => posts.map(&:to_h),
      )
    end
  end

  class S3Store
    # Pages, state/posts.json and locks/ share one bucket. state/ holds full feed
    # bodies; it stays unreadable from the site only because the prpr-antena
    # bucket policy denies the CloudFront OAI GetObject on state/* and locks/*
    # (release.yml checks this before deploying). The bucket lifecycle expires
    # only sites/, migration-preview/ and migration/, so the state is never
    # expired by it. Changing either bucket setting requires revisiting this.
    attr_reader :bucket, :prefix, :client

    def initialize(env = ENV)
      @bucket = env.fetch("MIGRATION_NEXT_BUCKET", "prpr-antena")
      @region = env.fetch("AWS_REGION", "ap-northeast-1")
      @prefix = env.fetch("MIGRATION_NEXT_PREFIX", "migration-preview").to_s.sub(%r{\A/+|/+\z}, "")
      @client = Aws::S3::Client.new(region: @region)

      if prefix.empty? && env["MIGRATION_NEXT_ALLOW_PUBLIC"] != "true"
        raise Error, "refusing to write public S3 keys; set MIGRATION_NEXT_ALLOW_PUBLIC=true only for cutover"
      end
    end

    def key(relative_path)
      [prefix, relative_path.to_s.sub(%r{\A/+}, "")].reject(&:empty?).join("/")
    end

    def state_key(relative_path)
      relative_path.to_s.sub(%r{\A/+}, "")
    end

    # nil only for a missing key. Any other failure (AccessDenied, network)
    # raises, so a caller never mistakes an unreadable object for an absent one.
    def read_if_exists(key)
      client.get_object(bucket: bucket, key: key).body.read
    rescue Aws::S3::Errors::NoSuchKey
      nil
    end

    def write(key, body, content_type: "application/json; charset=utf-8")
      client.put_object(bucket: bucket, key: key, body: body, content_type: content_type)
    end
  end

  class S3RunLock
    LOCK_KEY = "locks/migration-next-run.lock"
    # A lock older than this is treated as left behind by a killed execution.
    # Keep it longer than the Cloud Run task timeout (release.yml sets 30m):
    # a live execution must be killed by Cloud Run before another one may take
    # its lock over, or two executions would write the state concurrently.
    DEFAULT_STALE_AFTER_SECONDS = 3600

    def initialize(store, err: $stderr, stale_after: DEFAULT_STALE_AFTER_SECONDS, clock: -> { Time.now })
      @store = store
      @err = err
      @stale_after = stale_after
      @clock = clock
      @token = SecureRandom.uuid
      @acquired = false
    end

    def acquire
      attempts = 0

      loop do
        begin
          conditional_put("If-None-Match" => "*")
          @acquired = true
          return true
        rescue Aws::S3::Errors::ServiceError => e
          status = e.context&.http_response&.status_code
          raise unless [409, 412].include?(status)

          current, etag = read_lock
          if current && current["token"] == @token
            # A successful PUT whose response was lost may be retried by the SDK.
            @acquired = true
            return true
          end

          if attempts < 2 && (status == 409 || current.nil?)
            attempts += 1
            next
          end

          raise if current.nil?

          if stale?(current)
            return true if take_over(current, etag)

            # Another execution replaced the stale lock first; re-read it.
            if attempts < 2
              attempts += 1
              next
            end
          end

          @err.puts("WARN migration-next skipped: another execution owns the S3 lock (started_at=#{current.fetch('started_at', 'unknown')})")
          return false
        end
      end
    end

    def release
      return unless @acquired

      current, = read_lock
      return unless current && current["token"] == @token

      # Not conditional: a takeover between the read and this delete needs this
      # execution to have held the lock past the stale age, which the Cloud Run
      # task timeout rules out. A manual run (bin/run.rb from a laptop) has no
      # such limit; run it only while the scheduled job is paused.
      @store.client.delete_object(bucket: @store.bucket, key: LOCK_KEY)
      @acquired = false
    rescue StandardError => e
      @err.puts("WARN migration-next lock release failed: #{e.class}: #{e.message}; lock may need manual cleanup")
    end

    private

    def lock_body
      JSON.generate("token" => @token, "started_at" => @clock.call.utc.iso8601(6))
    end

    # The pinned aws-sdk-s3 does not model If-None-Match / If-Match for
    # PutObject. Add the S3 condition before the SDK signs and sends the request.
    def conditional_put(conditions)
      request = @store.client.build_request(
        :put_object,
        bucket: @store.bucket,
        key: LOCK_KEY,
        body: lock_body,
        content_type: "application/json; charset=utf-8",
      )
      conditions.each { |name, value| request.context.http_request.headers[name] = value }
      request.send_request
    end

    # Replace the stale lock only if it is still the object we read (If-Match),
    # so at most one of several waiting executions takes it over.
    def take_over(current, etag)
      return false if etag.nil? || etag.empty?

      conditional_put("If-Match" => etag)
      @acquired = true
      @err.puts("WARN migration-next took over a stale S3 lock (started_at=#{current.fetch('started_at', 'unknown')}, stale_after=#{@stale_after}s)")
      true
    rescue Aws::S3::Errors::ServiceError => e
      # 404: the stale lock was deleted after we read it; 409/412: someone else replaced it.
      raise unless [404, 409, 412].include?(e.context&.http_response&.status_code)

      false
    end

    def stale?(current)
      started_at = Time.iso8601(current["started_at"].to_s)
      @clock.call - started_at > @stale_after
    rescue ArgumentError
      # An unreadable start time can never expire on its own.
      @err.puts("WARN migration-next lock has an unreadable started_at=#{current['started_at'].inspect}; treating it as stale")
      true
    end

    # Returns [nil, nil] when no lock exists. A lock body that is not a JSON
    # object comes back as {} (no token, no started_at), so it is treated as
    # stale and taken over instead of blocking every run.
    def read_lock
      response = @store.client.get_object(bucket: @store.bucket, key: LOCK_KEY)
      body = response.body.read
      parsed = begin
        JSON.parse(body)
      rescue JSON::ParserError
        nil
      end
      unless parsed.is_a?(Hash)
        @err.puts("WARN migration-next lock body is not a JSON object (#{body.bytesize} bytes)")
        parsed = {}
      end
      [parsed, response.etag]
    rescue Aws::S3::Errors::NoSuchKey
      [nil, nil]
    end
  end

  class HtmlRenderer
    def initialize(catalog:, posts:, prefix:)
      @catalog = catalog
      @posts = posts
      @prefix = prefix
      @style = File.read(File.expand_path("../templates/style.html", __dir__))
    end

    def pages
      output = { "index.html" => category_page(@catalog.category_by_slug("vip")) }
      @catalog.categories.each do |category|
        output[category.slug] = category_page(category)
      end

      @catalog.enabled_sites.each do |site|
        site_posts = site.posts.sort_by { |post| [-post.id, post.created_at.to_f] }
        page_count = (site_posts.size / 50.0).ceil
        1.upto(page_count) do |page|
          suffix = page == 1 ? "" : "/#{page}"
          output["sites/#{site.id}#{suffix}"] = site_page(site, site_posts, page, page_count)
        end
      end
      output
    end

    private

    def category_page(category)
      raise Error, "category slug=vip is required for the index page" unless category

      now = Time.now
      site_ids = category.sites.select(&:enabled?).map(&:id).to_set
      fresh_posts = @posts.select do |post|
        site_ids.include?(post.site_id) && post.created_at >= now - 2 * 86_400 && post.created_at <= now
      end.sort_by { |post| -post.id }.first(150)

      site_cards = category.sites.map do |site|
        recent = site.posts.sort_by { |post| -post.created_at.to_f }.first(4)
        items = recent.map do |post|
          %(<li class="post_by_site">#{link(post.title, post.url, external: true, tracking: true)}</li>)
        end.join
        %(<li class="content"><div class="sitetitle">#{link(site.name, site.page_path)}</div><ul>#{items}<li class="post_by_site" style="text-align:right">#{link("続きを見る", site.page_path)}</li></ul></li>)
      end.join

      rows = []
      previous_day = nil
      fresh_posts.each do |post|
        day = tokyo_time(post.created_at).to_date
        if day != previous_day
          rows << %(<tr><td><b><ins>#{day.month}/#{day.day}</ins></b></td></tr>)
          previous_day = day
        end
        site = post.site
        desktop_title = link(post.title, post.url, external: true, tracking: true)
        mobile_title = link("#{truncate(post.title, 40)} (#{site.name})", post.url, external: true, tracking: true)
        rows << <<~HTML
          <tr>
            <td width="50px">#{tokyo_time(post.created_at).strftime("%H:%M")}</td>
            <td class="mawarikomi"><div class="visible_pc">#{desktop_title}</div><div class="visible_sp"><p style="height:70px">#{mobile_title}</p></div></td>
            <td><div class="visible_pc">#{link(site.name, site.url, external: true)}</div><div class="visible_sp"></div></td>
          </tr>
        HTML
      end

      body = <<~HTML
        <div id="base"><div class="container">
          <div class="left_column">#{category_menu(category)}
            <div id="top"><div id="headline_block"><table id="headline_body"><tbody>#{rows.join}</tbody></table></div>
              <div id="main"><ul id="sites">#{site_cards}</ul></div>
            </div>
          </div>
          <div class="right_column">#{recent_sidebar}</div>
        </div></div>
      HTML
      document("ぺろぺろあんてな", body)
    end

    def site_page(site, site_posts, page, page_count)
      posts = site_posts.slice((page - 1) * 50, 50) || []
      category = site.category
      post_items = posts.map do |post|
        image_tags = post.only_image_tags_by_content
        date = tokyo_time(post.created_at).strftime("%Y/%-m/%-d")
        <<~HTML
          <li class="tag_post">
            <div class="hidden" data="post-content">#{image_tags}</div>
            <div class="tag_post_image"></div>
            <div class="tag_post_body"><p><div>#{link(post.title, post.url, external: true)}</div><div class="text-right">#{link(site.name, site.url, external: true)}</div></p>
              <div class="scraped-images"></div><div class="category"><span>カテゴリ：</span>#{link(category.name, category_path(category.slug))}</div>
              <div><span>サイト(当サイト内へのリンク)：</span>#{link(site.name, site.page_path)}</div><div class="tag_post_tags"></div><div class="text-right">#{h(date)}</div>
            </div>
          </li>
        HTML
      end.join

      same_category = category.sites.map do |peer|
        %(<li>#{link("#{peer.name.empty? ? '登録名なし' : peer.name}(#{peer.posts_count})", peer.page_path)}</li>)
      end.join
      body = <<~HTML
        <div id="base">#{category_menu(category)}<div class="container">
          <div class="left_column"><p style="margin-bottom:10px">#{link("カテゴリ:#{category.name}", category_path(category.slug))} -&gt; #{h(site.name)}の記事一覧</p>
            <h1>#{link(site.name, site.url, external: true)} の記事</h1>
            #{pagination(site, page, page_count)}<ul>#{post_items}</ul>#{pagination(site, page, page_count)}
          </div>
          <div class="right_column">#{recent_sidebar}<h3 class="top-buffer">同一カテゴリのサイト</h3><ul class="list_in_side">#{same_category}</ul></div>
        </div></div>
        <script>
          document.addEventListener("DOMContentLoaded", function() {
            document.querySelectorAll(".tag_post").forEach(function(tagPost) {
              var source = tagPost.querySelector("[data=post-content]").innerHTML;
              var images = new DOMParser().parseFromString(source, "text/html").querySelectorAll("img");
              var target = tagPost.querySelector(".scraped-images");
              images.forEach(function(img) { var a = document.createElement("a"); a.href = img.getAttribute("src"); a.target = "_blank"; var copy = document.createElement("img"); copy.src = img.getAttribute("src"); a.appendChild(copy); target.appendChild(a); });
            });
          });
        </script>
      HTML
      canonical = page == 1 ? public_path(site.page_path) : nil
      document("#{h(site.name)}の記事一覧 | ぺろぺろあんてな", body, page: page, canonical: canonical)
    end

    def category_menu(current)
      links = @catalog.categories.map do |category|
        active = category.slug == current.slug ? "active" : ""
        %(<li>#{link(category.name, category_path(category.slug), css_class: active)}</li>)
      end.join
      %(<ul id="menu">#{links}</ul><h3 class="top-buffer">全カテゴリの新着記事</h3>)
    end

    def recent_sidebar
      recent = @posts.sort_by { |post| -post.created_at.to_f }.first(15).map do |post|
        %(<li>#{link(post.name_with_short, post.url, external: true)} (#{h(post.site.category.name)})</li>)
      end.join
      %(<h3 class="top-buffer">全カテゴリの新着記事</h3><ul class="list_in_side little-font">#{recent}</ul>)
    end

    def pagination(site, page, page_count)
      return "" if page_count <= 1

      links = (1..page_count).map do |number|
        if number == page
          %(<span aria-current="page">#{number}</span>)
        else
          %(<a href="#{h(public_path(site.page_path(number)))}">#{number}</a>)
        end
      end.join(" ")
      %(<nav class="pagination" aria-label="ページ">#{links}</nav>)
    end

    def document(title, body, page: nil, canonical: nil)
      # Preview copies must not compete with the live pages in search results.
      preview = !@prefix.to_s.empty?
      robots = preview || (page && page.to_i >= 2) ? '<meta content="noindex,follow" name="robots">' : ""
      canonical = canonical ? %(<link href="#{h(canonical)}" rel="canonical">) : ""
      <<~HTML
        <!DOCTYPE html><html><head><title>#{title}</title><meta name="viewport" content="width=device-width,initial-scale=1"><meta charset="utf-8">#{robots}#{canonical}#{@style}</head>
        <body><div class="navbar"><div id="nav"><div id="nav_left">#{link("ぺろぺろあんてな", public_path("index.html"), css_class: "site_name")}</div></div><br class="clearfix"></div>#{body}</body></html>
      HTML
    end

    def link(label, href, external: false, tracking: false, css_class: nil)
      path = external ? href.to_s : public_path(href)
      attrs = []
      attrs << %(href="#{h(path)}")
      attrs << 'target="_blank"' if external
      attrs << 'data-ob="true"' if tracking
      attrs << %(class="#{h(css_class)}") if css_class && !css_class.empty?
      %(<a #{attrs.join(" ")}>#{h(label)}</a>)
    end

    def public_path(path)
      base = @prefix.to_s.empty? ? "" : "/#{@prefix}"
      path = path.to_s
      path = "/#{path}" unless path.start_with?("/")
      "#{base}#{path}"
    end

    def category_path(slug) = public_path(slug.to_s.empty? ? "index.html" : slug)
    def h(value) = CGI.escapeHTML(value.to_s)
    def truncate(value, length) = value.to_s.length > length ? "#{value.to_s[0, length - 1]}…" : value.to_s
    def tokyo_time(time) = time.getlocal("+09:00")
  end

  class Runner
    DEFAULT_SITES_FILE = File.expand_path("../../config/sites.yml", __dir__)

    def initialize(env = ENV, log: $stdout, err: $stderr)
      @env = env
      @log = log
      @err = err
      @store = S3Store.new(env)
      @state_key = @store.state_key("state/posts.json")
    end

    def execute
      lock = S3RunLock.new(@store, err: @err)
      return unless lock.acquire

      begin
        catalog = Catalog.load(load_sites_yaml)
        state = load_state
        catalog.attach_posts(state.posts)
        fetch_new_posts(state, catalog)
        prune_old_posts(state, catalog)
        catalog.attach_posts(state.posts)

      # Save first so a later HTML upload failure can recover from the same post set.
        @store.write(@state_key, state.to_json)
        pages = HtmlRenderer.new(catalog: catalog, posts: state.posts, prefix: @store.prefix).pages
        pages.each do |relative_key, html|
          @store.write(@store.key(relative_key), html, content_type: "text/html; charset=utf-8")
        end
        @log.puts("migration-next uploaded #{pages.size} pages under s3://#{@store.bucket}/#{@store.prefix}/ and #{state.posts.size} posts to s3://#{@store.bucket}/#{@state_key}")
      ensure
        lock.release
      end
    end

    private

    # There is no import from Cloud SQL (user decision 2026-11-26): the first
    # run starts from the current RSS entries. Deleting state/posts.json is
    # also how to reset a broken state, since bucket versioning is off.
    # A file that exists but cannot be parsed still raises, so it is never
    # silently replaced by an empty set.
    def load_state
      text = @store.read_if_exists(@state_key)
      return State.load(text) if text

      @err.puts("WARN migration-next: s3://#{@store.bucket}/#{@state_key} does not exist; starting from an empty post set")
      State.new([])
    end

    def load_sites_yaml
      local_path = @env.fetch("MIGRATION_NEXT_SITES_FILE", DEFAULT_SITES_FILE)
      raise Error, "sites YAML does not exist: #{local_path}" unless File.file?(local_path)

      File.read(local_path)
    end

    def fetch_new_posts(state, catalog)
      titles = state.posts.filter_map(&:title).to_set
      urls = state.posts.filter_map(&:url).to_set
      next_id = state.next_id
      # Site.fetch uses ActiveRecord find_each, which processes primary keys in
      # ascending order. Preserve that order because the rendered feed sorts by
      # post id when multiple sites are fetched in the same batch.
      catalog.enabled_sites.sort_by(&:id).each do |site|
        begin
          next if site.rss_url.empty?

          response = Net::HTTP.get_response(URI(site.rss_url))
          feed = Feedjira.parse(response.body)
          next if feed.is_a?(Integer)

          inserted = 0
          Array(feed.entries).each do |entry|
            published_at = parse_published(entry.published)
            next if published_at && Time.now < published_at
            next if entry.url.to_s.empty?
            next if urls.include?(entry.url) || titles.include?(entry.title)

            state.posts << PostRecord.new(
              "id" => next_id,
              "site_id" => site.id,
              "title" => entry.title,
              "url" => entry.url,
              "summary" => entry.summary,
              "content" => cleaned_content(entry.content, entry.summary),
              "published_at" => published_at&.iso8601(6),
              "created_at" => Time.now.utc.iso8601(6),
              "updated_at" => Time.now.utc.iso8601(6),
            )
            urls << entry.url
            titles << entry.title if entry.title
            next_id += 1
            inserted += 1
          end
          @log.puts("#{site.name}: +#{inserted}")
        rescue StandardError => e
          @err.puts("WARN #{site.name} (site_id=#{site.id}): feed error #{e.class}: #{e.message}")
        end
      end
    end

    def parse_published(value)
      return nil if value.nil?
      return value.to_time if value.respond_to?(:to_time)

      Time.parse(value.to_s)
    rescue ArgumentError
      nil
    end

    def cleaned_content(content, summary)
      text = content
      if text
        text = text.gsub(%r!<script[^>]*>.*</script>!m, "")
        text = text.gsub(%r!<img src="[^"]*" width=.1. height=.1[^>]*>!m, "")
        text = text.gsub(%r_<!--[^-]*-->m_, "")
        text = text.gsub(%r!<a href=.http://2ch-c.net/[^>]*>[^<]*</a>!m, "")
        text = text.gsub(%r!<br( /)?>!, "")
        text = text.gsub(%r!<a [^>]*>続きを読む</a>!m, "")
        text = text.gsub(%r!&nbsp;!, "")
        text = text.gsub(%r!&gt;!, "")
      end

      doc = Nokogiri::HTML.parse(text || summary || "")
      doc.css("[id=related-title], .automatic-related, [id=article_mid], .kijinai").remove
      doc.to_html
    end

    def prune_old_posts(state, catalog)
      cutoff = Time.now - 5 * 86_400
      posts_by_site = state.posts.group_by(&:site_id)
      remove_ids = catalog.sites.flat_map do |site|
        site_posts = posts_by_site.fetch(site.id, [])
        next [] unless site_posts.size > 50

        site_posts.select { |post| post.created_at <= cutoff }.map(&:id)
      end.to_set
      state.posts.reject! { |post| remove_ids.include?(post.id) }
    end
  end
end
