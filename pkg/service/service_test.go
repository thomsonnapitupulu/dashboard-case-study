package service

import (
	"context"
	"testing"
	"time"

	"dashboard-case-study/pkg/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockEmployeeRepository is a mock implementation for testing
type MockEmployeeRepository struct {
	mock.Mock
}

func (m *MockEmployeeRepository) GetByID(ctx context.Context, employeeID string) (*models.Employee, error) {
	args := m.Called(ctx, employeeID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Employee), args.Error(1)
}

func (m *MockEmployeeRepository) GetHistory(ctx context.Context, employeeID string, asOf time.Time) ([]models.EmployeeHistory, error) {
	args := m.Called(ctx, employeeID, asOf)
	return args.Get(0).([]models.EmployeeHistory), args.Error(1)
}

// MockOrgRepository is a mock implementation for testing
type MockOrgRepository struct {
	mock.Mock
}

func (m *MockOrgRepository) GetUnitByID(ctx context.Context, unitID string) (*models.OrgUnit, error) {
	args := m.Called(ctx, unitID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.OrgUnit), args.Error(1)
}

func (m *MockOrgRepository) GetUnitAtTime(ctx context.Context, unitID string, asOf time.Time) (*models.OrgUnit, error) {
	args := m.Called(ctx, unitID, asOf)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.OrgUnit), args.Error(1)
}

func (m *MockOrgRepository) GetMapping(ctx context.Context, sourceUnitID string) (*models.OrgUnitMapping, error) {
	args := m.Called(ctx, sourceUnitID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.OrgUnitMapping), args.Error(1)
}

func (m *MockOrgRepository) FindMappingsByTarget(ctx context.Context, targetUnitID string) ([]models.OrgUnitMapping, error) {
	args := m.Called(ctx, targetUnitID)
	return args.Get(0).([]models.OrgUnitMapping), args.Error(1)
}

// TestSnapshotCapture tests the snapshot capture functionality
func TestSnapshotCapture(t *testing.T) {
	// Setup
	mockEmployeeRepo := new(MockEmployeeRepository)
	mockOrgRepo := new(MockOrgRepository)
	service := NewSnapshotService(mockEmployeeRepo, mockOrgRepo)

	ctx := context.Background()
	employeeID := "emp_123"
	timestamp := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)

	// Mock employee data
	employee := &models.Employee{
		EmployeeID:       employeeID,
		Name:             "John Doe",
		Email:            "john.doe@example.com",
		UnitID:           "unit_456",
		PerformanceGrade: "A",
		Role:             "Senior Manager",
		BirthDate:        time.Date(1989, 1, 1, 0, 0, 0, 0, time.UTC),
		HireDate:         time.Date(2019, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	// Mock org unit data
	orgUnit := &models.OrgUnit{
		UnitID:   "unit_456",
		UnitName: "Sales APAC",
		Path:     "root.apac.sales",
	}

	// Set expectations
	mockEmployeeRepo.On("GetByID", ctx, employeeID).Return(employee, nil)
	mockOrgRepo.On("GetUnitAtTime", ctx, "unit_456", timestamp).Return(orgUnit, nil)

	// Execute
	snapshot, err := service.CaptureSnapshot(ctx, employeeID, timestamp)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.Equal(t, employeeID, snapshot.EmployeeID)
	assert.Equal(t, "John Doe", snapshot.SnapshotCore["employee_name"])
	assert.Equal(t, "Sales APAC", snapshot.SnapshotCore["department"])
	assert.Equal(t, "A", snapshot.SnapshotCore["performance_grade"])
	assert.Equal(t, 35, snapshot.SnapshotCore["age"])            // Age at timestamp
	assert.InDelta(t, 4.8, snapshot.SnapshotCore["tenure"], 0.2) // Tenure at timestamp

	// Verify mocks
	mockEmployeeRepo.AssertExpectations(t)
	mockOrgRepo.AssertExpectations(t)
}

// TestCalculateAge tests the age calculation function
func TestCalculateAge(t *testing.T) {
	tests := []struct {
		name      string
		birthDate time.Time
		asOf      time.Time
		expected  int
	}{
		{
			name:      "Simple case",
			birthDate: time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
			asOf:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected:  34,
		},
		{
			name:      "Birthday not yet reached",
			birthDate: time.Date(1990, 6, 15, 0, 0, 0, 0, time.UTC),
			asOf:      time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			expected:  33,
		},
		{
			name:      "Birthday already passed",
			birthDate: time.Date(1990, 1, 15, 0, 0, 0, 0, time.UTC),
			asOf:      time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			expected:  34,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateAge(tt.birthDate, tt.asOf)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestCalculateTenure tests the tenure calculation function
func TestCalculateTenure(t *testing.T) {
	hireDate := time.Date(2019, 6, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)

	result := calculateTenure(hireDate, asOf)

	// Should be approximately 4.8 years
	assert.InDelta(t, 4.8, result, 0.1)
}

// Benchmark tests
func BenchmarkSnapshotCapture(b *testing.B) {
	mockEmployeeRepo := new(MockEmployeeRepository)
	mockOrgRepo := new(MockOrgRepository)
	service := NewSnapshotService(mockEmployeeRepo, mockOrgRepo)

	ctx := context.Background()
	employeeID := "emp_123"
	timestamp := time.Now()

	employee := &models.Employee{
		EmployeeID:       employeeID,
		Name:             "John Doe",
		UnitID:           "unit_456",
		PerformanceGrade: "A",
		BirthDate:        time.Date(1989, 1, 1, 0, 0, 0, 0, time.UTC),
		HireDate:         time.Date(2019, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	orgUnit := &models.OrgUnit{
		UnitID:   "unit_456",
		UnitName: "Sales APAC",
	}

	mockEmployeeRepo.On("GetByID", mock.Anything, mock.Anything).Return(employee, nil)
	mockOrgRepo.On("GetUnitAtTime", mock.Anything, mock.Anything, mock.Anything).Return(orgUnit, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.CaptureSnapshot(ctx, employeeID, timestamp)
	}
}
