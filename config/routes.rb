Rails.application.routes.draw do
  root 'categories#show'

  get '/toggle_pc_view' => 'home#toggle_pc_view', as: :toggle_pc_view

  # pageをpathパラメータにしている
  get 'sites/:id' => 'sites#show', as: :site
  get 'sites/:id/:page' => 'sites#show'

  resources :posts, only: %i(index show) do
    collection do
      resources :tags, only: [:index, :show], controller: 'posts/tags'
    end
  end

  namespace :admin do
    root 'home#index'

    resources :sessions, only: :create
    get 'login'  => 'sessions#new',  as: :login
    get 'logout' => 'sessions#destroy', as: :logout

    resources :categories
    resources :sites do
      member do
        get 'higher' => 'sites#move_order_higher', :as => 'higher'
        get 'lower'  => 'sites#move_order_lower',  :as => 'lower'
      end

      resources :posts, only: %i(index new create edit update destroy)
    end
  end

  get '/:id'  => 'categories#show', as: :category
end
