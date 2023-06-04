class Admin::PostsController < Admin::Base
  before_action :set_post, only: [:show, :edit, :update, :destroy]

  # GET /posts
  # GET /posts.json
  def index
    @posts = Post.order("created_at").page(params[:page]).per(20)
    @site = Site.find(params[:site_id])
  end

  # GET /posts/1
  # GET /posts/1.json
  def show
    @site = Site.find(params[:site_id])
  end

  # GET /posts/new
  def new
    @post = Post.new
    @site = Site.find(params[:site_id])
  end

  # GET /posts/1/edit
  def edit
    @site = Site.find(params[:site_id])
  end

  # POST /posts
  # POST /posts.json
  def create
    @post = Post.new(post_params)
    site = Site.find(params[:site_id])

    respond_to do |format|
      if @post.save
        format.html { redirect_to admin_site_posts_path(site), notice: 'Post was successfully created.' }
        format.json { render action: 'show', status: :created, location: [:admin, @post] }
      else
        format.html { render action: 'new' }
        format.json { render json: @post.errors, status: :unprocessable_entity }
      end
    end
  end

  # PATCH/PUT /posts/1
  # PATCH/PUT /posts/1.json
  def update
    site = Site.find(params[:site_id])

    respond_to do |format|
      if @post.update(post_params)
        format.html { redirect_to admin_site_posts_path(site), notice: 'Post was successfully updated.' }
        format.json { head :no_content }
      else
        format.html { render action: 'edit' }
        format.json { render json: @post.errors, status: :unprocessable_entity }
      end
    end
  end

  # DELETE /posts/1
  # DELETE /posts/1.json
  def destroy
    @post.destroy
    respond_to do |format|
      format.html { redirect_to admin_posts_url }
      format.json { head :no_content }
    end
  end

  private
    # Use callbacks to share common setup or constraints between actions.
    def set_post
      @post = Post.find(params[:id])
    end

    # Never trust parameters from the scary internet, only allow the white list through.
    def post_params
      params.require(:post).permit(:name, :url, :enable, :positon)
    end
end
