CREATE TABLE custom_fields (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity VARCHAR(50) NOT NULL,
    key VARCHAR(100) NOT NULL,
    label VARCHAR(100) NOT NULL,
    field_type VARCHAR(20) NOT NULL DEFAULT 'text',
    options JSONB NOT NULL DEFAULT '[]',
    required BOOLEAN NOT NULL DEFAULT false,
    sort_order INT NOT NULL DEFAULT 0,
    plugin_id UUID REFERENCES plugins(id) ON DELETE SET NULL,
    company_id VARCHAR(50) NOT NULL DEFAULT 'default',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (entity, key, company_id)
);

CREATE INDEX idx_custom_fields_entity ON custom_fields(entity, company_id, sort_order);

CREATE TABLE plugin_kv (
    plugin_id UUID NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
    key VARCHAR(200) NOT NULL,
    value JSONB NOT NULL DEFAULT 'null',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (plugin_id, key)
);
