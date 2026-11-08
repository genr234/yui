require "test_helper"

class DeploySettingsTest < ActiveSupport::TestCase
  test "thruster timeouts cover tus uploads and multi-GB downloads" do
    clear = YAML.safe_load_file(Rails.root.join("config/deploy.yml")).fetch("env").fetch("clear")

    assert_operator Integer(clear.fetch("RAILS_MAX_THREADS")), :>=, 8
    assert_operator Integer(clear.fetch("HTTP_READ_TIMEOUT")), :>=, 900
    assert_operator Integer(clear.fetch("HTTP_WRITE_TIMEOUT")), :>=, 86_400
    assert_operator Integer(clear.fetch("HTTP_IDLE_TIMEOUT")), :>=, 900

    deploy = Rails.root.join("config/deploy.yml").read
    assert_match(/keepalive gap between requests/i, deploy)
    assert_not_includes deploy, "mid-body"
    assert_match(/HTTP_WRITE_TIMEOUT is how long the client/i, deploy)
  end

  test "sqlite pool covers puma threads plus action cable workers, not forked jobs" do
    puma = Rails.root.join("config/puma.rb").read
    assert_includes puma, 'ENV.fetch("RAILS_MAX_THREADS"'
    assert_includes puma, "plugin :solid_queue"
    assert_no_match(/^\s*solid_queue_mode\b/, puma)

    initializer = Rails.root.join("config/initializers/action_cable.rb").read
    assert_includes initializer, 'ENV.fetch("ACTION_CABLE_WORKER_POOL_SIZE", 4)'
    assert_equal Integer(ENV.fetch("ACTION_CABLE_WORKER_POOL_SIZE", 4)), ActionCable.server.config.worker_pool_size

    with_env("RAILS_MAX_THREADS" => "8", "ACTION_CABLE_WORKER_POOL_SIZE" => nil) do
      pool = rendered_database.dig("production", "primary", "max_connections")
      assert_equal 12, pool
      assert_equal pool, rendered_database.dig("production", "queue", "max_connections")
      assert_equal pool, rendered_database.dig("production", "cable", "max_connections")

      worker_threads = rendered_queue.dig("production", "workers", 0, "threads")
      assert_equal 3, worker_threads
      # Solid Queue's own check is worker threads + 2 against the queue pool.
      assert_operator pool, :>=, worker_threads + 2
    end

    with_env("RAILS_MAX_THREADS" => "8", "ACTION_CABLE_WORKER_POOL_SIZE" => "6") do
      assert_equal 14, rendered_database.dig("production", "primary", "max_connections")
    end
  end

  private
    def rendered_database
      YAML.safe_load(ERB.new(Rails.root.join("config/database.yml").read).result, aliases: true)
    end

    def rendered_queue
      YAML.safe_load(ERB.new(Rails.root.join("config/queue.yml").read).result, aliases: true)
    end

    def with_env(overrides)
      previous = overrides.keys.to_h { |key| [ key, ENV[key] ] }
      overrides.each do |key, value|
        value.nil? ? ENV.delete(key) : ENV[key] = value
      end
      yield
    ensure
      previous.each do |key, value|
        value.nil? ? ENV.delete(key) : ENV[key] = value
      end
    end
end
