-- Migration: 001_initial_schema.up.sql
-- Description: Create initial database schema for snapshot system

-- Enable required extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "ltree"; -- For materialized paths

-- EMPLOYEES TABLE (Current State)
CREATE TABLE employees (
    employee_id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL UNIQUE,
    unit_id VARCHAR(255) NOT NULL,
    performance_grade VARCHAR(50),
    role VARCHAR(255),
    birth_date DATE NOT NULL,
    hire_date DATE NOT NULL,
    tenant_id VARCHAR(255) NOT NULL,
    updated_at TIMESTAMP DEFAULT NOW(),
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_employees_tenant ON employees(tenant_id);
CREATE INDEX idx_employees_unit ON employees(unit_id);

-- EMPLOYEE_HISTORY TABLE (SCD Type 2)
CREATE TABLE employee_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_id VARCHAR(255) NOT NULL,
    attribute_type VARCHAR(255) NOT NULL,
    attribute_value TEXT,
    valid_from TIMESTAMP NOT NULL,
    valid_to TIMESTAMP, -- NULL = current
    version_id VARCHAR(255) NOT NULL,
    tenant_id VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_employee_history_temporal ON employee_history(employee_id, valid_from, valid_to);
CREATE INDEX idx_employee_history_version ON employee_history(version_id);

-- ORG_UNITS_HISTORY TABLE (Organizational Structure with Versioning)
CREATE TABLE org_units_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    unit_id VARCHAR(255) NOT NULL,
    unit_name VARCHAR(255) NOT NULL,
    parent_unit_id VARCHAR(255),
    valid_from TIMESTAMP NOT NULL,
    valid_to TIMESTAMP, -- NULL = current
    is_active BOOLEAN DEFAULT TRUE,
    tenant_id VARCHAR(255) NOT NULL,
    path LTREE, -- Materialized path for fast hierarchy queries
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_org_units_temporal ON org_units_history(unit_id, valid_from, valid_to);
CREATE INDEX idx_org_units_path ON org_units_history USING GIST(path);
CREATE INDEX idx_org_units_tenant ON org_units_history(tenant_id);

-- ORG_UNIT_MAPPING TABLE (Tracks Organizational Restructures)
CREATE TABLE org_unit_mapping (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_unit_id VARCHAR(255) NOT NULL,
    target_unit_ids TEXT[] NOT NULL, -- PostgreSQL array for 1:N mappings
    relationship_type VARCHAR(50) NOT NULL CHECK (relationship_type IN ('RENAME', 'MERGE', 'SPLIT')),
    effective_date TIMESTAMP NOT NULL,
    description TEXT,
    tenant_id VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_org_mapping_source ON org_unit_mapping(source_unit_id, effective_date);
CREATE INDEX idx_org_mapping_target ON org_unit_mapping USING GIN(target_unit_ids);
CREATE INDEX idx_org_mapping_tenant ON org_unit_mapping(tenant_id);

-- SURVEY_RESPONSES TABLE (Responses with Snapshots)
CREATE TABLE survey_responses (
    response_id VARCHAR(255) PRIMARY KEY,
    survey_id VARCHAR(255) NOT NULL,
    employee_id VARCHAR(255) NOT NULL,
    submitted_at TIMESTAMP NOT NULL DEFAULT NOW(),
    snapshot_core JSONB NOT NULL, -- Core attributes snapshot
    version_id VARCHAR(255) NOT NULL, -- Links to employee_history
    answers JSONB NOT NULL,
    tenant_id VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Partitioning by submitted_at (monthly) for scalability
-- Note: In production, use pg_partman or manual partition management
CREATE INDEX idx_responses_tenant_survey ON survey_responses(tenant_id, survey_id, submitted_at DESC);
CREATE INDEX idx_responses_employee ON survey_responses(employee_id);
CREATE INDEX idx_responses_snapshot_gin ON survey_responses USING GIN(snapshot_core);

-- Expression indexes for common JSONB queries
CREATE INDEX idx_responses_department ON survey_responses((snapshot_core->>'department'));
CREATE INDEX idx_responses_grade ON survey_responses((snapshot_core->>'performance_grade'));
CREATE INDEX idx_responses_unit_id ON survey_responses((snapshot_core->>'unit_id'));

-- MATERIALIZED VIEWS FOR DASHBOARD AGGREGATIONS
CREATE MATERIALIZED VIEW mv_department_summary AS
SELECT 
    tenant_id,
    survey_id,
    snapshot_core->>'department' as department,
    snapshot_core->>'performance_grade' as grade,
    DATE_TRUNC('month', submitted_at) as month,
    COUNT(*) as response_count
FROM survey_responses
GROUP BY tenant_id, survey_id, department, grade, month;

CREATE UNIQUE INDEX idx_mv_dept_summary ON mv_department_summary(tenant_id, survey_id, department, grade, month);

-- ROW LEVEL SECURITY (Multi-Tenancy Isolation)
ALTER TABLE employees ENABLE ROW LEVEL SECURITY;
ALTER TABLE employee_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_units_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_unit_mapping ENABLE ROW LEVEL SECURITY;
ALTER TABLE survey_responses ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_employees ON employees
    USING (tenant_id = current_setting('app.tenant_id', TRUE));

CREATE POLICY tenant_isolation_employee_history ON employee_history
    USING (tenant_id = current_setting('app.tenant_id', TRUE));

CREATE POLICY tenant_isolation_org_units ON org_units_history
    USING (tenant_id = current_setting('app.tenant_id', TRUE));

CREATE POLICY tenant_isolation_org_mapping ON org_unit_mapping
    USING (tenant_id = current_setting('app.tenant_id', TRUE));

CREATE POLICY tenant_isolation_responses ON survey_responses
    USING (tenant_id = current_setting('app.tenant_id', TRUE));

-- HELPER FUNCTIONS
-- Function to refresh materialized views
CREATE OR REPLACE FUNCTION refresh_dashboard_views()
RETURNS void AS $$
BEGIN
    REFRESH MATERIALIZED VIEW CONCURRENTLY mv_department_summary;
END;
$$ LANGUAGE plpgsql;

-- SAMPLE DATA FOR TESTING
-- Will be added in seed script