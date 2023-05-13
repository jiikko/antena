class AdminUser < ApplicationRecord
  authenticates_with_sorcery!
end
