class Cron::FetchAndUpload
  def execute
    Site.fetch
    S3Uploader.new.execute
  end
end
