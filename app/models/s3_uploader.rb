class S3Uploader
  def initialize(s3)
    @s3 = Aws::S3::Resource.new(region:'us-west-2')
    @bucket_name = 'hoge'
  end

  def execute
    # /vip, /news, /anime, /talent, /adult
    # TODO:

    # /sites/:id/:page
    Site.enable.find_each do |site|
      page = 1
      loop do
        unless site.posts.page(page).exists?
          break
        end

        html_body = SitesController::ShowRenderer.new(site_id: site.id, page: page).render
        obj = @s3.bucket(@bucket_name).object("sites/#{site.id}/#{page}").put(body: html_body, content_type: 'text/html')
        page += 1
      end
    end
  end
end
