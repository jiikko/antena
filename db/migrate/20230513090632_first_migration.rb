class FirstMigration < ActiveRecord::Migration[7.0]
  def up
    create_table :posts do |t|
      t.string :name, index: true
      t.string :url, index: true
      t.text :summary
      t.text :content
      t.boolean :enable, default: true, null: false
      t.integer :positon
      t.integer :site_id, index: true


      t.timestamps
    end
    add_index :posts, :created_at
    add_index :posts, [:site_id, :created_at]

    create_table :sites do |t|
      t.string :name
      t.string :url
      t.string :rss_url
      t.text :description
      t.integer :category_id
      t.integer :position
      t.boolean :enable, default: true, null: false
      t.bigint :posts_count, :integer, default: 0

      t.timestamps
    end

    create_table :categories do |t|
      t.string :name
      t.string :slug
      t.string :identity

      t.timestamps
    end

    create_table :admin_users do |t|
      t.string :name,         :null => false
      t.string :email,            :default => nil
      t.string :crypted_password, :default => nil
      t.string :salt,             :default => nil

      t.timestamps
    end
  end

  def down
  end
end
