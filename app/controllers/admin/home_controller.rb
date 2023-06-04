class Admin::HomeController < Admin::BaseController
  def index
    cookies.permanent[:admin] = true # analyticsの解析が入らないようにするやつ
  end
end
