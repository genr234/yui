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
    resources :kiosk_commands, only: [ :create ]
  end

  namespace :api do
    namespace :kiosk do
      post "pair", to: "pairings#create"
      post "sync/push", to: "sync#push"
      get "sync/pull", to: "sync#pull"
      patch "commands/:id", to: "commands#update"
    end
  end

  # Yui Pad play sessions (kiosk API + phone endpoints)
  namespace :api do
    namespace :kiosk do
      resources :play_sessions, only: [ :create, :update, :destroy ] do
        get :ice, on: :member
        scope module: :play_sessions do
          post "players/:player_id/kick", to: "players#kick", as: :kick_player
          post "players/:player_id/host", to: "players#host", as: :host_player
        end
      end
    end
  end
  scope "play/:code", controller: :play, constraints: { code: /[A-Za-z]{4}/ } do
    get "/", action: :show, as: :play
    get "info", action: :info, as: :play_info
    post "join", action: :join, as: :play_join
    get "ice", action: :ice, as: :play_ice
  end

  # Define your application routes per the DSL in https://guides.rubyonrails.org/routing.html

  # Reveal health status on /up that returns 200 if the app boots with no exceptions, otherwise 500.
  # Can be used by load balancers and uptime monitors to verify that the app is live.
  get "up" => "rails/health#show", as: :rails_health_check

  # Render dynamic PWA files from app/views/pwa/* (remember to link manifest in application.html.erb)
  # get "manifest" => "rails/pwa#manifest", as: :pwa_manifest
  # get "service-worker" => "rails/pwa#service_worker", as: :pwa_service_worker

  # Defines the root path route ("/")
  # root "posts#index"
end
