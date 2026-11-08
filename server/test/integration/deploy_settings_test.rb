require "test_helper"

class DeploySettingsTest < ActiveSupport::TestCase
  test "puma threads and thruster timeouts cover multi-GB transfers" do
    deploy = YAML.safe_load_file(Rails.root.join("config/deploy.yml"))
    clear = deploy.fetch("env").fetch("clear")

    assert_operator Integer(clear.fetch("RAILS_MAX_THREADS")), :>=, 8
    assert_operator Integer(clear.fetch("HTTP_READ_TIMEOUT")), :>=, 900
    assert_operator Integer(clear.fetch("HTTP_WRITE_TIMEOUT")), :>=, 86_400
    assert_operator Integer(clear.fetch("HTTP_IDLE_TIMEOUT")), :>=, 900

    puma = Rails.root.join("config/puma.rb").read
    database = Rails.root.join("config/database.yml").read
    assert_includes puma, 'ENV.fetch("RAILS_MAX_THREADS"'
    assert_includes database, 'ENV.fetch("RAILS_MAX_THREADS")'
  end
end
