class Admin::SitesController < Admin::Base
  before_action :set_site, only: [:show, :edit, :update, :destroy]

  # GET /sites
  # GET /sites.json
  def index
    @sites = Site.includes(:category)
    if params[:site].nil?
      @sites = @sites.order("position")
    else
      @sites = @sites.where("category_id = ?", Category.find(params[:site].to_s)).order("position")
		end
  end

  # GET /sites/1
  # GET /sites/1.json
  def show
  end

  # GET /sites/new
  def new
    @site = Site.new
  end

  # GET /sites/1/edit
  def edit
  end

  # POST /sites
  # POST /sites.json
  def create
    @site = Site.new(site_params)

    respond_to do |format|
      if @site.save
        format.html { redirect_to [:admin, @site], notice: 'Site was successfully created.' }
        format.json { render action: 'show', status: :created, location: [:admin, @site] }
      else
        format.html { render action: 'new' }
        format.json { render json: @site.errors, status: :unprocessable_entity }
      end
    end
  end

  # PATCH/PUT /sites/1
  # PATCH/PUT /sites/1.json
  def update
    respond_to do |format|
      if @site.update(site_params)
        format.html { redirect_to [:admin, @site], notice: 'Site was successfully updated.' }
        format.json { head :no_content }
      else
        format.html { render action: 'edit' }
        format.json { render json: @site.errors, status: :unprocessable_entity }
      end
    end
  end

  # DELETE /sites/1
  # DELETE /sites/1.json
  def destroy
    @site.destroy
    respond_to do |format|
      format.html { redirect_to admin_sites_url }
      format.json { head :no_content }
    end
  end

  def move_order(method)
    @site = Site.find(params[:id])
    @site.send method

    redirect_to admin_sites_path(site: params[:site])
  end

  private
    # Use callbacks to share common setup or constraints between actions.
    def set_site
      @site = Site.find(params[:id])
    end

    # Never trust parameters from the scary internet, only allow the white list through.
    def site_params
      params.require(:site).permit(:name, :url, :description, :category_id, :position, :enable)
    end
end
