Rails.application.routes.draw do
  root "accounts#index"

  get "setup", to: "passkeys#new"
  post "passkeys/options", to: "passkeys#options", as: :passkey_options
  post "passkeys", to: "passkeys#create"

  get "login", to: "sessions#new"
  post "login/options", to: "sessions#options", as: :login_options
  post "login", to: "sessions#create"
  delete "logout", to: "sessions#destroy"

  resources :accounts, only: [ :index, :show, :new, :create, :edit, :update ] do
    resources :pairing_codes, only: [ :create ]
    resources :kiosk_commands, only: [ :create ]
  end

  namespace :api do
    namespace :kiosk do
      post "pair", to: "pairings#create"
      post "sync/push", to: "sync#push"
      get "sync/pull", to: "sync#pull"
      get "commands", to: "commands#index"
      patch "commands/:id", to: "commands#update"
    end
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
