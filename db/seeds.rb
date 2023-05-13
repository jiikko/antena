AdminUser.find_or_create_by!(name: "koji") do |admin_user|
  admin_user.password = "hogehoge"
end

category = Category.find_or_create_by!(name: "ＶＩＰ", slug: "vip")
%w[
  http://news4vip.livedoor.biz/
].each do |url|
  category.sites.find_or_create_by!(url: url)
end

Category.find_or_create_by!(name: "ニュー速", slug: "news")
Category.find_or_create_by!(name: "アニメ・ゲーム", slug: "anime")
Category.find_or_create_by!(name: "芸能・スポーツ", slug: "talent")
Category.find_or_create_by!(name: "アダルト", slug: "adult")
