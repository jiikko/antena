# This file is auto-generated from the current state of the database. Instead
# of editing this file, please use the migrations feature of Active Record to
# incrementally modify your database, and then regenerate this schema definition.
#
# This file is the source Rails uses to define your schema when running `bin/rails
# db:schema:load`. When creating a new database, `bin/rails db:schema:load` tends to
# be faster and is potentially less error prone than running all of your
# migrations from scratch. Old migrations may fail to apply correctly if those
# migrations use external dependencies or application code.
#
# It's strongly recommended that you check this file into your version control system.

ActiveRecord::Schema[7.1].define(version: 2023_05_13_090632) do
  create_table "admin_users", charset: "utf8mb4", collation: "utf8mb4_bin", force: :cascade do |t|
    t.string "name", null: false
    t.string "email"
    t.string "crypted_password"
    t.string "salt"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
  end

  create_table "categories", charset: "utf8mb4", collation: "utf8mb4_bin", force: :cascade do |t|
    t.string "name"
    t.string "slug"
    t.string "identity"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
  end

  create_table "posts", charset: "utf8mb4", collation: "utf8mb4_bin", force: :cascade do |t|
    t.string "name"
    t.string "url"
    t.text "summary"
    t.text "content"
    t.boolean "enable", default: true, null: false
    t.integer "positon"
    t.integer "site_id"
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
    t.index ["created_at"], name: "index_posts_on_created_at"
    t.index ["name"], name: "index_posts_on_name"
    t.index ["site_id", "created_at"], name: "index_posts_on_site_id_and_created_at"
    t.index ["site_id"], name: "index_posts_on_site_id"
    t.index ["url"], name: "index_posts_on_url"
  end

  create_table "sites", charset: "utf8mb4", collation: "utf8mb4_bin", force: :cascade do |t|
    t.string "name"
    t.string "url"
    t.string "rss_url"
    t.text "description"
    t.integer "category_id"
    t.integer "position"
    t.boolean "enable", default: true, null: false
    t.bigint "posts_count", default: 0
    t.bigint "integer", default: 0
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
  end

end
