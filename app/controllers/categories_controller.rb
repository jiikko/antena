class CategoriesController < ApplicationController
  def show
    @category_name = params[:id] || 'vip'
    @category = Category.find_by!(slug: @category_name)
    site_ids = Site.where(category: @category, enable: true).ids
    @posts = Post.preload(:site).
      where(site_id: site_ids,
            created_at: (DateTime.now-2.day)..DateTime.now).
      order("id DESC").
      select(Post::ACCESSABLE_COLUMNS).
      limit(150)

    if(request.smart_phone? and cookies[:force_pc_view].nil?)
      @posts = @posts.page(params[:page]).per(20)
    end

    @sites = @category.sites.order("position ASC")
    @site_id_posts_map = Post.posts_by(sites: @sites).group_by { |x| x.site_id }
  end
end
