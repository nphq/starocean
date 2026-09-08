CREATE TABLE partner_notes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_type VARCHAR(10) NOT NULL,
    partner_id UUID NOT NULL,
    note_type VARCHAR(20) NOT NULL DEFAULT '其他',
    content TEXT NOT NULL,
    next_follow_up DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    company_id VARCHAR(50) NOT NULL DEFAULT 'default'
);

CREATE INDEX idx_partner_notes_partner ON partner_notes(partner_type, partner_id);
CREATE INDEX idx_partner_notes_follow_up ON partner_notes(next_follow_up) WHERE next_follow_up IS NOT NULL;
CREATE INDEX idx_partner_notes_created ON partner_notes(created_at DESC);
CREATE INDEX idx_partner_notes_company ON partner_notes(company_id);
