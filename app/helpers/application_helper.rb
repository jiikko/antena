module ApplicationHelper
  def date_label(post)
    date = "#{post.created_at.month}/#{post.created_at.day}"
    label  =  "<tr><td><b><ins>#{date}</ins></b></td></tr>"

    if post.created_at.to_date == @current_date
      @current_date = post.created_at.to_date
      return nil
    else
      @current_date = post.created_at.to_date
      return raw(label)
    end
  end

  # @return [String, NilClass]
  def category_active_css(rendering_category: , current_category: )
    if rendering_category.slug == current_category.slug
      return 'active'
    end
  end
end
