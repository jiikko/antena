class Admin::HomeController < Admin::Base
  def index
    cookies.permanent[:admin] = true # analyticsの解析が入らないようにするやつ
  end
end
