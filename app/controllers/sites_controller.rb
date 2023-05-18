class SitesController < ApplicationController
  def show
    @site = Site.find(params[:id])
    @posts = @site.posts.includes(:site).page(params[:page]).per(10)

    # add_crumb 'トップページ', root_path
    # add_crumb "#{@site.name}の記事一覧"
  end
end
