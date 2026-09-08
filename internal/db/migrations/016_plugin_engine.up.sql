CREATE TABLE plugins (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    script TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT false,
    subscribed_events TEXT[] NOT NULL DEFAULT '{}',
    declared_slots TEXT[] NOT NULL DEFAULT '{}',
    timeout_ms INT NOT NULL DEFAULT 50,
    priority INT NOT NULL DEFAULT 100,
    version VARCHAR(20) NOT NULL DEFAULT '1.0.0',
    author VARCHAR(100) NOT NULL DEFAULT '',
    last_error TEXT,
    last_executed_at TIMESTAMPTZ,
    execution_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_plugins_enabled ON plugins(enabled) WHERE enabled = true;

CREATE TABLE plugin_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plugin_id UUID NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
    event_name VARCHAR(100) NOT NULL,
    duration_ms INT NOT NULL,
    success BOOLEAN NOT NULL,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_plugin_logs_plugin ON plugin_logs(plugin_id);
CREATE INDEX idx_plugin_logs_created ON plugin_logs(created_at);
