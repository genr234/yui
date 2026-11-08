# Pin npm packages by running ./bin/importmap

pin "application"
pin "@hotwired/turbo-rails", to: "turbo.min.js"
pin "@hotwired/stimulus", to: "stimulus.min.js"
pin "@hotwired/stimulus-loading", to: "stimulus-loading.js"
pin_all_from "app/javascript/controllers", under: "controllers"
pin "tus-js-client" # @4.3.1
# Shared chunk of tus-js-client@4.3.1 that jspm splits out (see the header of the vendored file).
pin "tus-js-client-shared", to: "tus-js-client-shared.js"
pin "js-base64" # @3.9.6
pin "querystringify" # @2.2.0
pin "requires-port" # @1.0.0
pin "url-parse" # @1.5.10
