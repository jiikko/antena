class S3Uploader
  class IndexPage
    def slug; ''; end
    def page_name; 'index.html'; end
  end

  class CategoryPage
    def initialize(slug)
      @slug = slug
    end
    def slug; @slug; end
    def page_name; @slug; end
  end

  def initialize
    @s3 = Aws::S3::Resource.new(region:'ap-northeast-1')
    @bucket_name = 'prpr-antena'
  end

  def execute
    # /vip, /news, /anime, /talent, /adult
    [IndexPage.new, Category.all.pluck(:slug).map { |x| CategoryPage.new(x) } ].flatten.each do |page_class|
      html_body = CategoriesController::ShowRenderer.new(slug: page_class.slug).render
      obj = @s3.bucket(@bucket_name).object(page_class.page_name).put(body: html_body, content_type: 'text/html')
    end

    # /sites/:id/:page
    Site.enabled.find_each do |site|
      page = 1
      loop do
        break unless site.posts.page(page).per(50).exists?

        html_body = SitesController::ShowRenderer.new(site_id: site.id, page: page).render
        object_path =
          if page == 1
            "sites/#{site.id}"
          else
            "sites/#{site.id}/#{page}"
          end
        obj = @s3.bucket(@bucket_name).object(object_path).put(body: html_body, content_type: 'text/html')
        page += 1
      end
    end
  end
end
