Rails.application.routes.draw do
  mount ActionCable.server => "/cable"

  root "accounts#index"

  get "setup", to: "passkeys#new"
  get "passkeys", to: "passkeys#index"
  post "passkeys/options", to: "passkeys#options", as: :passkey_options
  post "passkeys", to: "passkeys#create"
  delete "passkeys/:id", to: "passkeys#destroy", as: :passkey

  get "login", to: "sessions#new"
  post "login/options", to: "sessions#options", as: :login_options
  post "login", to: "sessions#create"
  delete "logout", to: "sessions#destroy"

  resources :accounts, only: [ :index, :show, :new, :create, :edit, :update, :destroy ] do
    member do
      delete "recent_commands", to: "accounts#clear_recent_commands"
      delete "recent_operations", to: "accounts#clear_recent_operations"
    end
    resources :pairing_codes, only: [ :create ]
    resources :phone_links, only: [ :create, :destroy ]
    resources :kiosk_commands, only: [ :create ]
    resources :kiosks, only: [ :show ] do
      member do
        post :restart_chrome
        post :update_now
        post :check_update
        get :remote
        post "remote/screenshot", action: :remote_screenshot, as: :remote_screenshot
        post "remote/input", action: :remote_input, as: :remote_input
        get "remote/commands/:command_id", action: :remote_command, as: :remote_command
      end
    end
    resources :linked_accounts, only: [ :new, :create, :show, :edit, :update, :destroy ] do
      collection { post :test }
      member do
        post :test
        post :sync
        get "cover/:remote_id", action: :cover, as: :cover
      end
      resources :linked_imports, only: [ :create, :destroy ]
    end
  end

  namespace :api do
    namespace :kiosk do
      post "pair", to: "pairings#create"
      post "sync/push", to: "sync#push"
      get "sync/pull", to: "sync#pull"
      patch "commands/:id", to: "commands#update"
      post "phone_link_codes", to: "phone_link_codes#create"
      post "telemetry", to: "telemetry#create"
    end
  end

  # Yui Pad play sessions (kiosk API + phone endpoints)
  namespace :api do
    namespace :kiosk do
      resources :play_sessions, only: [ :create, :show, :update, :destroy ] do
        get :ice, on: :member
        scope module: :play_sessions do
          post "players/:player_id/kick", to: "players#kick", as: :kick_player
          post "players/:player_id/host", to: "players#host", as: :host_player
          post "players/:player_id/speaker", to: "players#speaker", as: :speaker_player
          post "players/:player_id/pass_seat", to: "players#pass_seat", as: :pass_seat_player
          get "players/:player_id/profiles/:app_id", to: "profiles#show", as: :player_profile,
            constraints: { app_id: /[A-Za-z0-9._-]+/ }, format: false
          put "players/:player_id/profiles/:app_id", to: "profiles#update",
            constraints: { app_id: /[A-Za-z0-9._-]+/ }, format: false
        end
      end
    end
  end
  scope "play/:code", controller: :play, constraints: { code: /[A-Za-z]{4}/ } do
    get "/", action: :show, as: :play
    get "info", action: :info, as: :play_info
    post "join", action: :join, as: :play_join
    get "ice", action: :ice, as: :play_ice
    post "account", to: "play/accounts#create", as: :play_account
    patch "account", to: "play/accounts#update"
    delete "account", to: "play/accounts#destroy"
    post "profile_grants", to: "play/accounts#grant", as: :play_profile_grants
    delete "profile_grants/:app_id", to: "play/accounts#revoke_grant", as: :play_profile_grant,
      constraints: { app_id: /[A-Za-z0-9._-]+/ }, format: false
  end
  # Pad simulator: four pads in iframes (development and test only, never production).
  get "sim", to: "pad_sim#show", as: :pad_sim if Rails.env.local?

  # Define your application routes per the DSL in https://guides.rubyonrails.org/routing.html

  # Reveal health status on /up that returns 200 if the app boots with no exceptions, otherwise 500.
  # Can be used by load balancers and uptime monitors to verify that the app is live.
  get "up" => "rails/health#show", as: :rails_health_check

  # Asset library (Phase 4): browser list/delete, tus uploads, public covers, kiosk API.
  resources :accounts, only: [] do
    resources :assets, only: [ :index, :update, :destroy ]
    match "uploads", to: "asset_uploads#options", via: :options
    post "uploads", to: "asset_uploads#create", as: :asset_uploads
    get "uploads/:id", to: "asset_uploads#show", as: :asset_upload
    patch "uploads/:id", to: "asset_uploads#update"
    delete "uploads/:id", to: "asset_uploads#destroy"
    match "uploads/:id", to: "asset_uploads#options", via: :options
  end
  get "media_covers/:key", to: "media_covers#show", as: :media_cover, format: false, constraints: { key: /\h{32}\.(png|jpg|gif|webp)/ }
  get "covers/linked/:key", to: "covers#linked", as: :linked_cover, format: false, constraints: { key: /\d+-\h{8}\.(png|jpg|webp)/ }
  get "covers/:game_id.png", to: "covers#show", as: :cover, format: false, constraints: { game_id: /[A-Z0-9]{6}/ }
  namespace :api do
    namespace :kiosk do
      get "assets", to: "assets#index"
      get "assets/:id/download", to: "assets#download", as: :asset_download
      post "assets/:id/request", to: "assets#request_asset", as: :asset_request
      patch "assets/:id/status", to: "assets#update_status", as: :asset_status
      # Save-file backups, one per app id + runtime id (contracts §4 "Native sessions").
      scope constraints: { app_id: %r{[^/]+}, runtime: %r{[^/]+} }, format: false do
        get "saves/:app_id/:runtime", to: "saves#show", as: :save
        get "saves/:app_id/:runtime/download", to: "saves#download", as: :save_download
        put "saves/:app_id/:runtime", to: "saves#update"
      end
      # App save blobs (contracts §6 "App saves"); `?player_id=` selects the player scope.
      scope constraints: { app_id: %r{[^/]+}, key: %r{[^/]+} }, format: false do
        get "app_saves/:app_id", to: "app_saves#index", as: :app_saves
        get "app_saves/:app_id/:key", to: "app_saves#show", as: :app_save
        put "app_saves/:app_id/:key", to: "app_saves#update"
        delete "app_saves/:app_id/:key", to: "app_saves#destroy"
      end
    end
  end

  # Render dynamic PWA files from app/views/pwa/* (remember to link manifest in application.html.erb)
  # get "manifest" => "rails/pwa#manifest", as: :pwa_manifest
  # get "service-worker" => "rails/pwa#service_worker", as: :pwa_service_worker

  # Defines the root path route ("/")
  # root "posts#index"
end
