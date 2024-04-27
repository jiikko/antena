class S3Uploader
  class IndexPage
    def slug = ''
    def page_name = 'index.html'
  end

  class CategoryPage
    def initialize(slug)
      @slug = slug
    end

    attr_reader :slug

    def page_name = @slug
  end

  def initialize
    @s3 = Aws::S3::Resource.new(region: 'ap-northeast-1')
    @bucket_name = Rails.configuration.x.s3_bucket_name
  end

  def execute
    # /vip, /news, /anime, /talent, /adult
    [IndexPage.new, Category.all.pluck(:slug).map { |x| CategoryPage.new(x) }].flatten.each do |page_class|
      html_body = CategoriesController::ShowRenderer.new(slug: page_class.slug).render
      @s3.bucket(@bucket_name).object(page_class.page_name).put(body: html_body, content_type: 'text/html')
    end

    # /sites/:id/:page
    Site.enabled.find_each do |site|
      page = 1
      loop do
        break unless site.posts.page(page).per(50).exists?

        html_body = SitesController::ShowRenderer.new(site_id: site.id, page:).render
        object_path =
          if page == 1
            "sites/#{site.id}"
          else
            "sites/#{site.id}/#{page}"
          end
        @s3.bucket(@bucket_name).object(object_path).put(body: html_body, content_type: 'text/html')
        page += 1
      end
    end
  end
end
