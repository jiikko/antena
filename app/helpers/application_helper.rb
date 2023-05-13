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

  def category_active_css(category)
    return("active") if(category.slug == params[:id])

    if request.url == root_url
      return("active") if category.slug == 'vip'
    end
  end
end
