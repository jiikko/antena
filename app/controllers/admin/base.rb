class Admin::Base < ActionController::Base
  before_action :login_required
  layout 'admin/application'

  def move_order_higher
    move_order(:move_higher)
  end

  def move_order_lower
    move_order(:move_lower)
  end

  private

  def login_required
    unless current_user
      redirect_to admin_login_path
    end
  end
end
