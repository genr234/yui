# Channel callbacks run outside Puma's request threads and check out their own
# database connections. config/database.yml sizes pools with the same env var.
# The gem default is 4; keep that unless the pool formula changes with it.
Rails.application.config.action_cable.worker_pool_size =
  Integer(ENV.fetch("ACTION_CABLE_WORKER_POOL_SIZE", 4))
