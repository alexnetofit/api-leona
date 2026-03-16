CREATE TABLE IF NOT EXISTS instances (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    phone TEXT,
    status TEXT NOT NULL DEFAULT 'disconnected',
    webhook_url TEXT,
    proxy_ip TEXT,
    proxy_provider TEXT,
    proxy_session_id TEXT,
    worker_id TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS workers (
    id TEXT PRIMARY KEY,
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    max_instances INTEGER NOT NULL DEFAULT 50,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_heartbeat_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS proxy_pool (
    id SERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    ip_address TEXT NOT NULL UNIQUE,
    session_id TEXT,
    status TEXT NOT NULL DEFAULT 'available',
    assigned_instance_id TEXT REFERENCES instances(id) ON DELETE SET NULL,
    health_score INTEGER NOT NULL DEFAULT 100,
    last_check_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS webhook_logs (
    id SERIAL PRIMARY KEY,
    instance_id TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    event TEXT NOT NULL,
    payload TEXT,
    status_code INTEGER,
    attempts INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS api_keys (
    id SERIAL PRIMARY KEY,
    key_hash TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    permissions TEXT,
    rate_limit INTEGER NOT NULL DEFAULT 60,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_instances_status ON instances(status);
CREATE INDEX IF NOT EXISTS idx_instances_worker_id ON instances(worker_id);
CREATE INDEX IF NOT EXISTS idx_proxy_pool_status ON proxy_pool(status);
CREATE INDEX IF NOT EXISTS idx_proxy_pool_provider ON proxy_pool(provider);
CREATE INDEX IF NOT EXISTS idx_proxy_pool_assigned ON proxy_pool(assigned_instance_id);
CREATE INDEX IF NOT EXISTS idx_webhook_logs_instance ON webhook_logs(instance_id);
CREATE INDEX IF NOT EXISTS idx_webhook_logs_created ON webhook_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash);
