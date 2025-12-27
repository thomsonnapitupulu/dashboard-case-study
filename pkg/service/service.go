package service

import (
	"context"
	"fmt"
	"time"

	"dashboard-case-study/pkg/models"

	"dashboard-case-study/pkg/repository"
)

// SnapshotService handles snapshot capture logic
type SnapshotService struct {
	employeeRepo repository.EmployeeRepository
	orgRepo      repository.OrgRepository
}

func NewSnapshotService(
	employeeRepo repository.EmployeeRepository,
	orgRepo repository.OrgRepository,
) *SnapshotService {
	return &SnapshotService{
		employeeRepo: employeeRepo,
		orgRepo:      orgRepo,
	}
}

// CaptureSnapshot captures employee and org state at given timestamp
func (s *SnapshotService) CaptureSnapshot(ctx context.Context, employeeID string, timestamp time.Time) (*models.Snapshot, error) {
	// Get current employee state
	employee, err := s.employeeRepo.GetByID(ctx, employeeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get employee: %w", err)
	}

	// Get org unit at this time
	orgUnit, err := s.orgRepo.GetUnitAtTime(ctx, employee.UnitID, timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to get org unit: %w", err)
	}

	// Build core snapshot (20 critical attributes)
	snapshotCore := s.buildCoreSnapshot(employee, orgUnit, timestamp)

	// Generate version ID for this point in time
	versionID := s.generateVersionID(employeeID, timestamp)

	return &models.Snapshot{
		EmployeeID:   employeeID,
		SnapshotCore: snapshotCore,
		VersionID:    versionID,
		Timestamp:    timestamp,
	}, nil
}

func (s *SnapshotService) buildCoreSnapshot(
	employee *models.Employee,
	orgUnit *models.OrgUnit,
	timestamp time.Time,
) map[string]interface{} {
	return map[string]interface{}{
		// Employee identity
		"employee_name":  employee.Name,
		"employee_email": employee.Email,

		// Organizational context
		"department": orgUnit.UnitName,
		"unit_id":    orgUnit.UnitID,
		"unit_path":  orgUnit.Path,

		// Performance & role
		"performance_grade": employee.PerformanceGrade,
		"role":              employee.Role,

		// Demographics (calculated at response time)
		"age":    calculateAge(employee.BirthDate, timestamp),
		"tenure": calculateTenure(employee.HireDate, timestamp),

		// Metadata
		"snapshot_version": "1.0",
		"snapshot_time":    timestamp.Format(time.RFC3339),
	}
}

func (s *SnapshotService) generateVersionID(employeeID string, timestamp time.Time) string {
	return fmt.Sprintf("%s_%d", employeeID, timestamp.Unix())
}

// Helper functions
func calculateAge(birthDate, asOf time.Time) int {
	age := asOf.Year() - birthDate.Year()
	if asOf.YearDay() < birthDate.YearDay() {
		age--
	}
	return age
}

func calculateTenure(hireDate, asOf time.Time) float64 {
	years := asOf.Sub(hireDate).Hours() / (24 * 365.25)
	return float64(int(years*10)) / 10 // Round to 1 decimal
}

// DashboardService handles dashboard queries
type DashboardService struct {
	responseRepo repository.ResponseRepository
	orgRepo      repository.OrgRepository
	orgMapper    *OrgMapper
}

func NewDashboardService(
	responseRepo repository.ResponseRepository,
	orgRepo repository.OrgRepository,
) *DashboardService {
	return &DashboardService{
		responseRepo: responseRepo,
		orgRepo:      orgRepo,
		orgMapper:    NewOrgMapper(orgRepo),
	}
}

// Query executes a dashboard query with filter mode support
func (s *DashboardService) Query(ctx context.Context, query models.DashboardQuery) (*models.DashboardResult, error) {
	switch query.FilterMode {
	case models.FilterModeHistorical:
		return s.queryHistorical(ctx, query)
	case models.FilterModeCurrent:
		return s.queryCurrent(ctx, query)
	case models.FilterModeHybrid:
		return s.queryHybrid(ctx, query)
	default:
		return nil, fmt.Errorf("invalid filter mode: %s", query.FilterMode)
	}
}

func (s *DashboardService) queryHistorical(ctx context.Context, query models.DashboardQuery) (*models.DashboardResult, error) {
	// Direct query on snapshot_core
	responses, err := s.responseRepo.Query(ctx, query)
	if err != nil {
		return nil, err
	}

	return &models.DashboardResult{
		Responses: responses,
		Count:     len(responses),
	}, nil
}

func (s *DashboardService) queryCurrent(ctx context.Context, query models.DashboardQuery) (*models.DashboardResult, error) {
	// Translate current org structure to historical unit IDs
	if dept, ok := query.Filters["department"].(string); ok {
		historicalUnitIDs, err := s.orgMapper.MapCurrentToHistorical(ctx, dept)
		if err != nil {
			return nil, fmt.Errorf("failed to map current to historical: %w", err)
		}

		// Replace department filter with unit_id IN clause
		delete(query.Filters, "department")
		query.Filters["unit_id"] = historicalUnitIDs
	}

	responses, err := s.responseRepo.Query(ctx, query)
	if err != nil {
		return nil, err
	}

	return &models.DashboardResult{
		Responses: responses,
		Count:     len(responses),
	}, nil
}

func (s *DashboardService) queryHybrid(ctx context.Context, query models.DashboardQuery) (*models.DashboardResult, error) {
	// Execute both historical and current queries
	historicalResult, err := s.queryHistorical(ctx, query)
	if err != nil {
		return nil, err
	}

	currentResult, err := s.queryCurrent(ctx, query)
	if err != nil {
		return nil, err
	}

	// Merge results with provenance
	merged := s.mergeResults(historicalResult, currentResult)
	return merged, nil
}

func (s *DashboardService) mergeResults(historical, current *models.DashboardResult) *models.DashboardResult {
	// Combine responses (deduplicate by response_id)
	seen := make(map[string]bool)
	var merged []models.Response

	for _, r := range historical.Responses {
		if !seen[r.ResponseID] {
			merged = append(merged, r)
			seen[r.ResponseID] = true
		}
	}

	for _, r := range current.Responses {
		if !seen[r.ResponseID] {
			merged = append(merged, r)
			seen[r.ResponseID] = true
		}
	}

	return &models.DashboardResult{
		Responses: merged,
		Count:     len(merged),
		Provenance: &models.ProvenanceInfo{
			HistoricalCount: historical.Count,
			CurrentCount:    current.Count,
		},
	}
}

// OrgMapper handles organizational unit mapping
type OrgMapper struct {
	orgRepo repository.OrgRepository
	cache   map[string][]string // Cache of current → historical mappings
}

func NewOrgMapper(orgRepo repository.OrgRepository) *OrgMapper {
	return &OrgMapper{
		orgRepo: orgRepo,
		cache:   make(map[string][]string),
	}
}

// MapCurrentToHistorical maps current unit name to all historical unit IDs
func (m *OrgMapper) MapCurrentToHistorical(ctx context.Context, currentUnitName string) ([]string, error) {
	// Check cache
	if cached, ok := m.cache[currentUnitName]; ok {
		return cached, nil
	}

	// Find current unit by name
	// In production, this would query org_units_history WHERE unit_name = X AND valid_to IS NULL
	// For POC, we'll use a simplified approach

	// TODO: Implement backward graph traversal
	// For now, return single unit
	result := []string{currentUnitName}

	// Cache result
	m.cache[currentUnitName] = result

	return result, nil
}

// ResponseService handles response submission
type ResponseService struct {
	responseRepo repository.ResponseRepository
	snapshotSvc  *SnapshotService
}

func NewResponseService(
	responseRepo repository.ResponseRepository,
	snapshotSvc *SnapshotService,
) *ResponseService {
	return &ResponseService{
		responseRepo: responseRepo,
		snapshotSvc:  snapshotSvc,
	}
}

// Submit creates a new response with snapshot
func (s *ResponseService) Submit(ctx context.Context, surveyID, employeeID, tenantID string, answers map[string]interface{}) (*models.Response, error) {
	// Capture snapshot at submission time
	snapshot, err := s.snapshotSvc.CaptureSnapshot(ctx, employeeID, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to capture snapshot: %w", err)
	}

	// Create response
	response := &models.Response{
		ResponseID:   repository.GenerateID(),
		SurveyID:     surveyID,
		EmployeeID:   employeeID,
		SnapshotCore: snapshot.SnapshotCore,
		VersionID:    snapshot.VersionID,
		TenantID:     tenantID,
	}

	// Store in database
	err = s.responseRepo.Create(ctx, response)
	if err != nil {
		return nil, fmt.Errorf("failed to create response: %w", err)
	}

	return response, nil
}
