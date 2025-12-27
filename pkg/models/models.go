package models

import (
	"encoding/json"
	"time"
)

// FilterMode defines how dashboard filters should be interpreted
type FilterMode string

const (
	FilterModeHistorical FilterMode = "HISTORICAL" // Data as-of response time
	FilterModeCurrent    FilterMode = "CURRENT"    // Map to current org structure
	FilterModeHybrid     FilterMode = "HYBRID"     // Show both with breakdown
)

// MappingType defines organizational unit relationship types
type MappingType string

const (
	MappingTypeRename MappingType = "RENAME" // 1:1 (same unit, new name)
	MappingTypeMerge  MappingType = "MERGE"  // N:1 (multiple → one)
	MappingTypeSplit  MappingType = "SPLIT"  // 1:N (one → multiple)
)

// Response represents a survey response with snapshot
type Response struct {
	ResponseID   string                 `json:"response_id" db:"response_id"`
	SurveyID     string                 `json:"survey_id" db:"survey_id"`
	EmployeeID   string                 `json:"employee_id" db:"employee_id"`
	SubmittedAt  time.Time              `json:"submitted_at" db:"submitted_at"`
	SnapshotCore map[string]interface{} `json:"snapshot_core" db:"snapshot_core"`
	VersionID    string                 `json:"version_id" db:"version_id"`
	Answers      json.RawMessage        `json:"answers" db:"answers"`
	TenantID     string                 `json:"tenant_id" db:"tenant_id"`
	CreatedAt    time.Time              `json:"created_at" db:"created_at"`
}

// Employee represents current employee state
type Employee struct {
	EmployeeID       string    `json:"employee_id" db:"employee_id"`
	Name             string    `json:"name" db:"name"`
	Email            string    `json:"email" db:"email"`
	UnitID           string    `json:"unit_id" db:"unit_id"`
	PerformanceGrade string    `json:"performance_grade" db:"performance_grade"`
	Role             string    `json:"role" db:"role"`
	BirthDate        time.Time `json:"birth_date" db:"birth_date"`
	HireDate         time.Time `json:"hire_date" db:"hire_date"`
	TenantID         string    `json:"tenant_id" db:"tenant_id"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}

// EmployeeHistory represents versioned employee attributes (SCD Type 2)
type EmployeeHistory struct {
	ID             string     `json:"id" db:"id"`
	EmployeeID     string     `json:"employee_id" db:"employee_id"`
	AttributeType  string     `json:"attribute_type" db:"attribute_type"`
	AttributeValue string     `json:"attribute_value" db:"attribute_value"`
	ValidFrom      time.Time  `json:"valid_from" db:"valid_from"`
	ValidTo        *time.Time `json:"valid_to" db:"valid_to"` // NULL = current
	VersionID      string     `json:"version_id" db:"version_id"`
	TenantID       string     `json:"tenant_id" db:"tenant_id"`
}

// OrgUnit represents organizational unit
type OrgUnit struct {
	UnitID       string     `json:"unit_id" db:"unit_id"`
	UnitName     string     `json:"unit_name" db:"unit_name"`
	ParentUnitID *string    `json:"parent_unit_id" db:"parent_unit_id"`
	ValidFrom    time.Time  `json:"valid_from" db:"valid_from"`
	ValidTo      *time.Time `json:"valid_to" db:"valid_to"` // NULL = current
	IsActive     bool       `json:"is_active" db:"is_active"`
	TenantID     string     `json:"tenant_id" db:"tenant_id"`
	Path         string     `json:"path" db:"path"` // Materialized path (ltree)
}

// OrgUnitMapping tracks organizational restructures
type OrgUnitMapping struct {
	ID               string      `json:"id" db:"id"`
	SourceUnitID     string      `json:"source_unit_id" db:"source_unit_id"`
	TargetUnitIDs    []string    `json:"target_unit_ids" db:"target_unit_ids"`
	RelationshipType MappingType `json:"relationship_type" db:"relationship_type"`
	EffectiveDate    time.Time   `json:"effective_date" db:"effective_date"`
	Description      string      `json:"description" db:"description"`
	TenantID         string      `json:"tenant_id" db:"tenant_id"`
	CreatedAt        time.Time   `json:"created_at" db:"created_at"`
}

// Snapshot represents captured employee/org state
type Snapshot struct {
	EmployeeID   string                 `json:"employee_id"`
	SnapshotCore map[string]interface{} `json:"snapshot_core"`
	VersionID    string                 `json:"version_id"`
	Timestamp    time.Time              `json:"timestamp"`
}

// DashboardQuery represents a dashboard filter request
type DashboardQuery struct {
	Filters    map[string]interface{} `json:"filters"`
	FilterMode FilterMode             `json:"filter_mode"`
	TimeRange  TimeRange              `json:"time_range"`
	TenantID   string                 `json:"tenant_id"`
}

// TimeRange represents a date range
type TimeRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// DashboardResult represents query results
type DashboardResult struct {
	Responses    []Response             `json:"responses"`
	Count        int                    `json:"count"`
	Aggregations map[string]interface{} `json:"aggregations,omitempty"`
	Provenance   *ProvenanceInfo        `json:"provenance,omitempty"`
}

// ProvenanceInfo tracks data sources in hybrid mode
type ProvenanceInfo struct {
	HistoricalCount int      `json:"historical_count"`
	CurrentCount    int      `json:"current_count"`
	HistoricalUnits []string `json:"historical_units"`
}

// SubmitResponseRequest represents API request to submit response
type SubmitResponseRequest struct {
	EmployeeID string                 `json:"employee_id"`
	Answers    map[string]interface{} `json:"answers"`
}

// SubmitResponseResponse represents API response
type SubmitResponseResponse struct {
	ResponseID  string    `json:"response_id"`
	SubmittedAt time.Time `json:"submitted_at"`
}
