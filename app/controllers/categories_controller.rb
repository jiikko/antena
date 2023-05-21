class CategoriesController < ApplicationController
  class ShowRenderer
    POST_COLUMNS = %i(name site_id created_at url id)

    def initialize(slug: )
      @category = Category.find_by!(slug: slug.presence || 'vip')
      site_ids = Site.enabled.where(category: @category).ids
      @posts = Post.preload(:site).
        where(site_id: site_ids, created_at: (DateTime.now - 2.day)..DateTime.now). order("id DESC").
        select(POST_COLUMNS).
        limit(150)

      @sites = @category.sites.order("position ASC")
      @site_id_posts_map = Post.posts_by(sites: @sites).group_by { |x| x.site_id }
    end

    def render
      ApplicationController.renderer.render_to_string(
        template: "categories/show",
        assigns: { category: @category, posts: @posts, sites: @sites, site_id_posts_map: @site_id_posts_map },
        format: "html",
      )
    end
  end

  def show
    html = ShowRenderer.new(slug: params[:id]).render
    render html: html
  end
end
