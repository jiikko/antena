class Category < ApplicationRecord
  has_many :sites
  has_many :posts, through: :sites

  validates :name, presence: true, uniqueness: true
  validates :slug, presence: true, uniqueness: true
end
