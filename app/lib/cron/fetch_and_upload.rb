class Cron::FetchAndUpload
  def execute
    Site.fetch
    S3Uploader.new.execute
    export_sites_yaml
  end

  private

  # RDB切り離し移行用。本体バッチを止めないよう、失敗してもログだけ残して続行する
  def export_sites_yaml
    SiteYamlExporter.new.execute
  rescue => e
    Rails.logger.error "[Cron::FetchAndUpload] SiteYamlExporter failed: #{e.class}: #{e.message}"
  end
end
