# Code Examples

## Table of Contents

1. [Submitting a Response with Snapshot](#1-submitting-a-response-with-snapshot)
2. [Querying Dashboard - Historical Mode](#2-querying-dashboard---historical-mode)
3. [Querying Dashboard - Current Mode](#3-querying-dashboard---current-mode)
4. [Handling Organizational Restructure](#4-handling-organizational-restructure)
5. [Multi-Tenant Query](#5-multi-tenant-query)

---

## 1. Submitting a Response with Snapshot

### Scenario
Employee "John Doe" submits a survey response. System captures snapshot of his current state.

### Code Example

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/engagerocket/snapshot-poc/pkg/models"
	"github.com/engagerocket/snapshot-poc/pkg/repository"
	"github.com/engagerocket/snapshot-poc/pkg/service"
)

func exampleSubmitResponse(db *sql.DB) {
	ctx := context.Background()

	// Initialize repositories
	employeeRepo := repository.NewPostgresEmployeeRepository(db)
	orgRepo := repository.NewPostgresOrgRepository(db)
	responseRepo := repository.NewPostgresResponseRepository(db)

	// Initialize services
	snapshotSvc := service.NewSnapshotService(employeeRepo, orgRepo)
	responseSvc := service.NewResponseService(responseRepo, snapshotSvc)

	// Submit response
	response, err := responseSvc.Submit(
		ctx,
		"survey_001",           // surveyID
		"emp_john_doe",         // employeeID
		"tenant_acme",          // tenantID
		map[string]interface{}{ // answers
			"q1_engagement": 9,
			"q2_satisfaction": "Very Satisfied",
		},
	)

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Response submitted successfully!\n")
	fmt.Printf("Response ID: %s\n", response.ResponseID)
	fmt.Printf("Submitted At: %s\n", response.SubmittedAt)
	fmt.Printf("Snapshot: %+v\n", response.SnapshotCore)
}
```

### What Gets Stored

```json
{
  "response_id": "resp_abc123",
  "survey_id": "survey_001",
  "employee_id": "emp_john_doe",
  "submitted_at": "2024-03-15T10:30:00Z",
  "snapshot_core": {
    "employee_name": "John Doe",
    "employee_email": "john.doe@acme.com",
    "department": "Sales APAC",
    "unit_id": "unit_123",
    "performance_grade": "A",
    "role": "Senior Sales Manager",
    "age": 35,
    "tenure": 5.2
  },
  "version_id": "emp_john_doe_1710497400",
  "answers": {
    "q1_engagement": 9,
    "q2_satisfaction": "Very Satisfied"
  }
}
```

---

## 2. Querying Dashboard - Historical Mode

### Scenario
Manager wants to see responses from employees who were in "Sales APAC" department at the time they responded.

### Code Example

```go
func exampleQueryHistorical(db *sql.DB) {
	ctx := context.Background()

	// Initialize
	responseRepo := repository.NewPostgresResponseRepository(db)
	orgRepo := repository.NewPostgresOrgRepository(db)
	dashboardSvc := service.NewDashboardService(responseRepo, orgRepo)

	// Build query
	query := models.DashboardQuery{
		Filters: map[string]interface{}{
			"department":        "Sales APAC",
			"performance_grade": "A",
		},
		FilterMode: models.FilterModeHistorical,
		TimeRange: models.TimeRange{
			From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
		},
		TenantID: "tenant_acme",
	}

	// Execute query
	result, err := dashboardSvc.Query(ctx, query)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Found %d responses\n", result.Count)
	for _, resp := range result.Responses {
		fmt.Printf("- %s: %s (Grade: %s)\n",
			resp.SubmittedAt.Format("2006-01-02"),
			resp.SnapshotCore["employee_name"],
			resp.SnapshotCore["performance_grade"],
		)
	}
}
```

### Generated SQL

```sql
SELECT response_id, survey_id, employee_id, submitted_at,
       snapshot_core, version_id, answers, tenant_id, created_at
FROM survey_responses
WHERE tenant_id = 'tenant_acme'
  AND submitted_at BETWEEN '2024-01-01' AND '2024-12-31'
  AND snapshot_core->>'department' = 'Sales APAC'
  AND snapshot_core->>'performance_grade' = 'A'
ORDER BY submitted_at DESC
LIMIT 1000;
```

---

## 3. Querying Dashboard - Current Mode

### Scenario
After "Sales APAC" was renamed to "Revenue APAC", manager wants to see ALL responses from this team, using the current name.

### Code Example

```go
func exampleQueryCurrent(db *sql.DB) {
	ctx := context.Background()

	responseRepo := repository.NewPostgresResponseRepository(db)
	orgRepo := repository.NewPostgresOrgRepository(db)
	dashboardSvc := service.NewDashboardService(responseRepo, orgRepo)

	// Query using CURRENT department name
	query := models.DashboardQuery{
		Filters: map[string]interface{}{
			"department": "Revenue APAC", // Current name
		},
		FilterMode: models.FilterModeCurrent, // KEY: Current mode
		TimeRange: models.TimeRange{
			From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
		},
		TenantID: "tenant_acme",
	}

	result, err := dashboardSvc.Query(ctx, query)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Found %d responses from Revenue APAC (including historical 'Sales APAC')\n", result.Count)
}
```

### What Happens Internally

1. **Filter resolution:**
   - User selected: "Revenue APAC" (current name)
   - System maps: "Revenue APAC" → ["unit_456", "unit_123"] (current + historical)
   
2. **Query translation:**
   ```sql
   -- Instead of:
   WHERE snapshot_core->>'department' = 'Revenue APAC'
   
   -- Executes:
   WHERE snapshot_core->>'unit_id' IN ('unit_456', 'unit_123')
   ```

3. **Result includes:**
   - Responses when it was "Sales APAC" (Jan-May 2024)
   - Responses after rename to "Revenue APAC" (Jun-Dec 2024)

---

## 4. Handling Organizational Restructure

### Scenario
"Sales APAC" is split into "SEA Sales" and "ANZ Sales". Create mapping and query.

### Code Example

```go
func exampleOrgRestructure(db *sql.DB) error {
	ctx := context.Background()

	// Step 1: Create org restructure mapping
	mapping := &models.OrgUnitMapping{
		ID:            repository.GenerateID(),
		SourceUnitID:  "unit_123", // Sales APAC
		TargetUnitIDs: []string{"unit_456", "unit_457"}, // SEA Sales, ANZ Sales
		RelationshipType: models.MappingTypeSplit,
		EffectiveDate: time.Date(2024, 9, 1, 0, 0, 0, 0, time.UTC),
		Description:   "Split Sales APAC into regional teams",
		TenantID:      "tenant_acme",
	}

	// Store mapping
	_, err := db.ExecContext(ctx, `
		INSERT INTO org_unit_mapping (
			id, source_unit_id, target_unit_ids, relationship_type,
			effective_date, description, tenant_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		mapping.ID,
		mapping.SourceUnitID,
		pq.Array(mapping.TargetUnitIDs),
		mapping.RelationshipType,
		mapping.EffectiveDate,
		mapping.Description,
		mapping.TenantID,
	)

	if err != nil {
		return fmt.Errorf("failed to create mapping: %w", err)
	}

	// Step 2: Create new org units
	for i, unitID := range mapping.TargetUnitIDs {
		unitName := []string{"SEA Sales", "ANZ Sales"}[i]
		
		_, err = db.ExecContext(ctx, `
			INSERT INTO org_units_history (
				unit_id, unit_name, parent_unit_id, valid_from,
				valid_to, is_active, tenant_id, path
			) VALUES ($1, $2, $3, $4, NULL, TRUE, $5, $6)
		`,
			unitID,
			unitName,
			nil, // Top-level unit
			mapping.EffectiveDate,
			mapping.TenantID,
			unitName, // Simplified path
		)

		if err != nil {
			return fmt.Errorf("failed to create unit: %w", err)
		}
	}

	// Step 3: Close old unit
	_, err = db.ExecContext(ctx, `
		UPDATE org_units_history
		SET valid_to = $1, is_active = FALSE
		WHERE unit_id = $2 AND valid_to IS NULL
	`, mapping.EffectiveDate, mapping.SourceUnitID)

	fmt.Println("Org restructure completed successfully")
	return err
}
```

### Query After Split

```go
// User queries "SEA Sales" in current mode
query := models.DashboardQuery{
	Filters: map[string]interface{}{
		"department": "SEA Sales",
	},
	FilterMode: models.FilterModeCurrent,
	// ...
}

// System resolves:
// "SEA Sales" (unit_456) ← came from "Sales APAC" (unit_123)
// Query includes responses where unit_id IN ('unit_456', 'unit_123')
```

---

## 5. Multi-Tenant Query

### Scenario
Ensure tenant isolation is enforced for all queries.

### Code Example

```go
func exampleMultiTenant(db *sql.DB) {
	ctx := context.Background()

	// Set tenant context (would come from JWT token in production)
	_, err := db.ExecContext(ctx, "SET app.tenant_id = 'tenant_acme'")
	if err != nil {
		fmt.Printf("Failed to set tenant: %v\n", err)
		return
	}

	// All subsequent queries automatically filtered by tenant_id
	// due to Row Level Security policies

	responseRepo := repository.NewPostgresResponseRepository(db)
	
	// This query ONLY returns data for tenant_acme
	// Even if we try to access tenant_xyz data, RLS blocks it
	query := models.DashboardQuery{
		Filters:    map[string]interface{}{},
		FilterMode: models.FilterModeHistorical,
		TimeRange: models.TimeRange{
			From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Now(),
		},
		TenantID: "tenant_acme",
	}

	responses, err := responseRepo.Query(ctx, query)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Found %d responses for tenant_acme\n", len(responses))
}
```

---

## Complete End-to-End Example

### Scenario
1. Employee submits response
2. Org restructure happens
3. Manager queries dashboard with current structure

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
	"github.com/engagerocket/snapshot-poc/pkg/models"
	"github.com/engagerocket/snapshot-poc/pkg/repository"
	"github.com/engagerocket/snapshot-poc/pkg/service"
)

func main() {
	// Connect to database
	db, err := sql.Open("postgres", "postgres://user:pass@localhost/snapshots?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	// Initialize services
	employeeRepo := repository.NewPostgresEmployeeRepository(db)
	orgRepo := repository.NewPostgresOrgRepository(db)
	responseRepo := repository.NewPostgresResponseRepository(db)

	snapshotSvc := service.NewSnapshotService(employeeRepo, orgRepo)
	responseSvc := service.NewResponseService(responseRepo, snapshotSvc)
	dashboardSvc := service.NewDashboardService(responseRepo, orgRepo)

	// ========================================
	// STEP 1: Employee submits response
	// ========================================
	fmt.Println("Step 1: Submitting response...")
	
	response, err := responseSvc.Submit(
		ctx,
		"survey_001",
		"emp_alice",
		"tenant_acme",
		map[string]interface{}{
			"q1_engagement": 8,
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("✓ Response submitted: %s\n", response.ResponseID)
	fmt.Printf("  Department at response time: %s\n", response.SnapshotCore["department"])

	// ========================================
	// STEP 2: Org restructure happens
	// ========================================
	fmt.Println("\nStep 2: Department renamed...")
	
	// Simulate rename: "Sales APAC" → "Revenue APAC"
	// (In production, this would come from HR system webhook)
	
	time.Sleep(1 * time.Second)

	// ========================================
	// STEP 3: Manager queries dashboard
	// ========================================
	fmt.Println("\nStep 3: Querying dashboard with current structure...")

	query := models.DashboardQuery{
		Filters: map[string]interface{}{
			"department": "Revenue APAC", // Current name
		},
		FilterMode: models.FilterModeCurrent,
		TimeRange: models.TimeRange{
			From: time.Now().Add(-24 * time.Hour),
			To:   time.Now(),
		},
		TenantID: "tenant_acme",
	}

	result, err := dashboardSvc.Query(ctx, query)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("✓ Found %d responses\n", result.Count)
	fmt.Println("  Includes responses from when it was 'Sales APAC'")
}
```

---

## Testing Examples

See `tests/` directory for comprehensive test suite.

**Run specific test:**
```bash
go test -v ./pkg/service -run TestSnapshotCapture
```

**Run with coverage:**
```bash
go test -cover ./...
```