class S3Uploader
  def initialize
    @s3 = Aws::S3::Resource.new(region:'ap-northeast-1')
    @bucket_name = 'prpr-antena'
  end

  def execute
    # /vip, /news, /anime, /talent, /adult
    # TODO:

    # /sites/:id/:page
    Site.enable.find_each do |site|
      page = 1
      loop do
        break unless site.posts.page(page).per(20).exists?

        html_body = SitesController::ShowRenderer.new(site_id: site.id, page: page).render
        obj = @s3.bucket(@bucket_name).object("sites/#{site.id}/#{page}").put(body: html_body, content_type: 'text/html')
        page += 1
      end
    end
  end
end
