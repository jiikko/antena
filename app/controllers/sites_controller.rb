class SitesController < ApplicationController
  class ShowRenderer
    def initialize(site_id: , page: )
      @page = page
      @site = Site.find(site_id)
      @posts = @site.posts.includes(:site).page(page).per(50)
    end

    # @return [String] html
    def render
      ApplicationController.renderer.render_to_string(
        template: "sites/show",
        assigns: { site: @site, posts: @posts, page: @page },
        locals: { params: { page: @page } },
        format: "html",
      )
    end
  end

  def show
    html = ShowRenderer.new(site_id: params[:id], page: params[:page]).render
    render html: html

    # add_crumb 'トップページ', root_path
    # add_crumb "#{@site.name}の記事一覧"
  end
end
