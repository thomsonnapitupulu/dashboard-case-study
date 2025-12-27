package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"dashboard-case-study/pkg/models"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ResponseRepository handles survey response persistence
type ResponseRepository interface {
	Create(ctx context.Context, response *models.Response) error
	GetByID(ctx context.Context, responseID string) (*models.Response, error)
	Query(ctx context.Context, query models.DashboardQuery) ([]models.Response, error)
}

// EmployeeRepository handles employee data
type EmployeeRepository interface {
	GetByID(ctx context.Context, employeeID string) (*models.Employee, error)
	GetHistory(ctx context.Context, employeeID string, asOf time.Time) ([]models.EmployeeHistory, error)
}

// OrgRepository handles organizational structure
type OrgRepository interface {
	GetUnitByID(ctx context.Context, unitID string) (*models.OrgUnit, error)
	GetUnitAtTime(ctx context.Context, unitID string, asOf time.Time) (*models.OrgUnit, error)
	GetMapping(ctx context.Context, sourceUnitID string) (*models.OrgUnitMapping, error)
	FindMappingsByTarget(ctx context.Context, targetUnitID string) ([]models.OrgUnitMapping, error)
}

// PostgresResponseRepository implements ResponseRepository
type PostgresResponseRepository struct {
	db *sql.DB
}

func NewPostgresResponseRepository(db *sql.DB) *PostgresResponseRepository {
	return &PostgresResponseRepository{db: db}
}

func (r *PostgresResponseRepository) Create(ctx context.Context, response *models.Response) error {
	// Marshal snapshot_core to JSONB
	snapshotJSON, err := json.Marshal(response.SnapshotCore)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot_core: %w", err)
	}

	answersJSON, err := json.Marshal(response.Answers)
	if err != nil {
		return fmt.Errorf("failed to marshal answers: %w", err)
	}

	query := `
		INSERT INTO survey_responses (
			response_id, survey_id, employee_id, submitted_at, 
			snapshot_core, version_id, answers, tenant_id
		) VALUES ($1, $2, $3, NOW(), $4, $5, $6, $7)
		RETURNING submitted_at, created_at
	`

	err = r.db.QueryRowContext(ctx, query,
		response.ResponseID,
		response.SurveyID,
		response.EmployeeID,
		snapshotJSON,
		response.VersionID,
		answersJSON,
		response.TenantID,
	).Scan(&response.SubmittedAt, &response.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to create response: %w", err)
	}

	return nil
}

func (r *PostgresResponseRepository) GetByID(ctx context.Context, responseID string) (*models.Response, error) {
	query := `
		SELECT response_id, survey_id, employee_id, submitted_at,
		       snapshot_core, version_id, answers, tenant_id, created_at
		FROM survey_responses
		WHERE response_id = $1
	`

	var response models.Response
	var snapshotJSON, answersJSON []byte

	err := r.db.QueryRowContext(ctx, query, responseID).Scan(
		&response.ResponseID,
		&response.SurveyID,
		&response.EmployeeID,
		&response.SubmittedAt,
		&snapshotJSON,
		&response.VersionID,
		&answersJSON,
		&response.TenantID,
		&response.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("response not found: %s", responseID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get response: %w", err)
	}

	// Unmarshal JSONB fields
	if err := json.Unmarshal(snapshotJSON, &response.SnapshotCore); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot_core: %w", err)
	}
	if err := json.Unmarshal(answersJSON, &response.Answers); err != nil {
		return nil, fmt.Errorf("failed to unmarshal answers: %w", err)
	}

	return &response, nil
}

func (r *PostgresResponseRepository) Query(ctx context.Context, q models.DashboardQuery) ([]models.Response, error) {
	// Build dynamic query based on filters
	baseQuery := `
		SELECT response_id, survey_id, employee_id, submitted_at,
		       snapshot_core, version_id, answers, tenant_id, created_at
		FROM survey_responses
		WHERE tenant_id = $1
		  AND submitted_at BETWEEN $2 AND $3
	`

	args := []interface{}{q.TenantID, q.TimeRange.From, q.TimeRange.To}
	argIndex := 4

	// Add JSONB filters
	for field, value := range q.Filters {
		baseQuery += fmt.Sprintf(" AND snapshot_core->>$%d = $%d", argIndex, argIndex+1)
		args = append(args, field, fmt.Sprintf("%v", value))
		argIndex += 2
	}

	baseQuery += " ORDER BY submitted_at DESC LIMIT 1000"

	rows, err := r.db.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query responses: %w", err)
	}
	defer rows.Close()

	var responses []models.Response
	for rows.Next() {
		var resp models.Response
		var snapshotJSON, answersJSON []byte

		err := rows.Scan(
			&resp.ResponseID,
			&resp.SurveyID,
			&resp.EmployeeID,
			&resp.SubmittedAt,
			&snapshotJSON,
			&resp.VersionID,
			&answersJSON,
			&resp.TenantID,
			&resp.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Unmarshal JSONB
		if err := json.Unmarshal(snapshotJSON, &resp.SnapshotCore); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(answersJSON, &resp.Answers); err != nil {
			return nil, err
		}

		responses = append(responses, resp)
	}

	return responses, nil
}

// PostgresEmployeeRepository implements EmployeeRepository
type PostgresEmployeeRepository struct {
	db *sql.DB
}

func NewPostgresEmployeeRepository(db *sql.DB) *PostgresEmployeeRepository {
	return &PostgresEmployeeRepository{db: db}
}

func (r *PostgresEmployeeRepository) GetByID(ctx context.Context, employeeID string) (*models.Employee, error) {
	query := `
		SELECT employee_id, name, email, unit_id, performance_grade,
		       role, birth_date, hire_date, tenant_id, updated_at
		FROM employees
		WHERE employee_id = $1
	`

	var emp models.Employee
	err := r.db.QueryRowContext(ctx, query, employeeID).Scan(
		&emp.EmployeeID,
		&emp.Name,
		&emp.Email,
		&emp.UnitID,
		&emp.PerformanceGrade,
		&emp.Role,
		&emp.BirthDate,
		&emp.HireDate,
		&emp.TenantID,
		&emp.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("employee not found: %s", employeeID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get employee: %w", err)
	}

	return &emp, nil
}

func (r *PostgresEmployeeRepository) GetHistory(ctx context.Context, employeeID string, asOf time.Time) ([]models.EmployeeHistory, error) {
	query := `
		SELECT id, employee_id, attribute_type, attribute_value,
		       valid_from, valid_to, version_id, tenant_id
		FROM employee_history
		WHERE employee_id = $1
		  AND valid_from <= $2
		  AND (valid_to IS NULL OR valid_to > $2)
	`

	rows, err := r.db.QueryContext(ctx, query, employeeID, asOf)
	if err != nil {
		return nil, fmt.Errorf("failed to query employee history: %w", err)
	}
	defer rows.Close()

	var history []models.EmployeeHistory
	for rows.Next() {
		var h models.EmployeeHistory
		err := rows.Scan(
			&h.ID,
			&h.EmployeeID,
			&h.AttributeType,
			&h.AttributeValue,
			&h.ValidFrom,
			&h.ValidTo,
			&h.VersionID,
			&h.TenantID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan history row: %w", err)
		}
		history = append(history, h)
	}

	return history, nil
}

// PostgresOrgRepository implements OrgRepository
type PostgresOrgRepository struct {
	db *sql.DB
}

func NewPostgresOrgRepository(db *sql.DB) *PostgresOrgRepository {
	return &PostgresOrgRepository{db: db}
}

func (r *PostgresOrgRepository) GetUnitByID(ctx context.Context, unitID string) (*models.OrgUnit, error) {
	query := `
		SELECT unit_id, unit_name, parent_unit_id, valid_from, valid_to,
		       is_active, tenant_id, path
		FROM org_units_history
		WHERE unit_id = $1
		  AND valid_to IS NULL
	`

	return r.scanOrgUnit(ctx, query, unitID)
}

func (r *PostgresOrgRepository) GetUnitAtTime(ctx context.Context, unitID string, asOf time.Time) (*models.OrgUnit, error) {
	query := `
		SELECT unit_id, unit_name, parent_unit_id, valid_from, valid_to,
		       is_active, tenant_id, path
		FROM org_units_history
		WHERE unit_id = $1
		  AND valid_from <= $2
		  AND (valid_to IS NULL OR valid_to > $2)
	`

	return r.scanOrgUnit(ctx, query, unitID, asOf)
}

func (r *PostgresOrgRepository) scanOrgUnit(ctx context.Context, query string, args ...interface{}) (*models.OrgUnit, error) {
	var unit models.OrgUnit
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&unit.UnitID,
		&unit.UnitName,
		&unit.ParentUnitID,
		&unit.ValidFrom,
		&unit.ValidTo,
		&unit.IsActive,
		&unit.TenantID,
		&unit.Path,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("org unit not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get org unit: %w", err)
	}

	return &unit, nil
}

func (r *PostgresOrgRepository) GetMapping(ctx context.Context, sourceUnitID string) (*models.OrgUnitMapping, error) {
	query := `
		SELECT id, source_unit_id, target_unit_ids, relationship_type,
		       effective_date, description, tenant_id, created_at
		FROM org_unit_mapping
		WHERE source_unit_id = $1
		ORDER BY effective_date DESC
		LIMIT 1
	`

	var mapping models.OrgUnitMapping
	err := r.db.QueryRowContext(ctx, query, sourceUnitID).Scan(
		&mapping.ID,
		&mapping.SourceUnitID,
		pq.Array(&mapping.TargetUnitIDs),
		&mapping.RelationshipType,
		&mapping.EffectiveDate,
		&mapping.Description,
		&mapping.TenantID,
		&mapping.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No mapping found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get mapping: %w", err)
	}

	return &mapping, nil
}

func (r *PostgresOrgRepository) FindMappingsByTarget(ctx context.Context, targetUnitID string) ([]models.OrgUnitMapping, error) {
	query := `
		SELECT id, source_unit_id, target_unit_ids, relationship_type,
		       effective_date, description, tenant_id, created_at
		FROM org_unit_mapping
		WHERE $1 = ANY(target_unit_ids)
		ORDER BY effective_date DESC
	`

	rows, err := r.db.QueryContext(ctx, query, targetUnitID)
	if err != nil {
		return nil, fmt.Errorf("failed to query mappings: %w", err)
	}
	defer rows.Close()

	var mappings []models.OrgUnitMapping
	for rows.Next() {
		var m models.OrgUnitMapping
		err := rows.Scan(
			&m.ID,
			&m.SourceUnitID,
			pq.Array(&m.TargetUnitIDs),
			&m.RelationshipType,
			&m.EffectiveDate,
			&m.Description,
			&m.TenantID,
			&m.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan mapping: %w", err)
		}
		mappings = append(mappings, m)
	}

	return mappings, nil
}

// GenerateID generates a new UUID
func GenerateID() string {
	return uuid.New().String()
}
