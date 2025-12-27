# Solution to EngageRocket Dashboard Snapshot Problem

Author: Thomson (thomson.p.napitupulu@gmail.com)
## Thought Process Design
---
Before answering the problem I want to the thought process design I used, it is RESHADED framework (https://www.educative.io/courses/grokking-the-system-design-interview/the-reshaded-approach-for-system-design). This is the framework I usually used for solving any technical system design problem. I might tailor the framework based on the problem needs.

## R - Requirements

### **Functional Requirements**
- Capture employee/org state at response submission time
- Filter responses by historical attributes (dept, grade, tenure, etc.)
- Support both "historical view" and "current view" filtering
- Handle org restructures (rename, merge, split)

### **Non-functional Requirements**
- Performance: <2s dashboard query response time
- Scale: Millions records of responses, hundreds of attributes
- Data integrity: Immutable once submitted
- Multi-tenancy: Isolated client data

---

## E - Estimation

### **Assumptions**
- 1M responses/year across all clients
- 500 attributes per employee (including org hierarchy)
- 100-1000 concurrent dashboard users
- Survey duration: extended, up to 1 year
- Org changes: ~5% of units/quarter

### **Resource Estimates**
- **Full snapshot:** ~50KB/response = 50GB/year
- **Temporal tables:** ~10KB/response = 10GB/year
- **Hybrid:** ~20KB/response = 20GB/year
- Query load: ~10-50 queries/second during peak

---

## S - Storage Schema
### **Important Tables**
![Diagram of important tables to solve the problem](/assets/images/erd.png)

## H - High Level Design
### **Architecture Components**
![Diagram of important tables to solve the problem](/assets/images/logic-flow.png)

### **Key Components**
1. **Snapshot Service** - Capture state on response submit
2. **Temporal Query Engine** - Reconstruct historical state (track changes)
3. **Filter Resolver** - Translate current → historical units
4. **Materialized Views** - Cache common aggregations

## A - APIs

### **Core API Contracts**

```typescript
// 1. Snapshot Capture
POST /api/v1/responses/{surveyId}/submit
Request: { 
  employeeId: string,
  answers: object,
  timestamp: Date 
}
→ Triggers: captureSnapshot(employeeId, timestamp)

// 2. Dashboard Query
POST /api/v1/dashboards/{dashboardId}/query
Request: {
  filters: { 
    department: "Sales", 
    grade: "A" 
  },
  filterMode: "HISTORICAL" | "CURRENT" | "HYBRID",
  timeRange: { from: Date, to: Date }
}
Response: { 
  aggregations: object,
  breakdowns: object[],
  count: number 
}

// 3. Org Structure Query
GET /api/v1/org-structure?asOf={timestamp}&mode={current|historical}
Response: { 
  units: object[],
  hierarchy: object,
  mappings: object[] 
}
```

---

## D - Detailed Design

### **Critical Decision Points**

#### **1. Snapshot Strategy: Hybrid Approach**
- Store **core attributes** in snapshot (20 fields): dept, grade, age, tenure
- Reference **extended attributes** via version_id
- **Rationale:** Balance speed (80% queries) vs flexibility (20% queries)

#### **2. Org Change Handling**

```
Scenario: Unit renamed "Sales APAC" → "Revenue APAC"

Solution:
├── Keep unit_id=123 unchanged
├── Create new version with new name
├── Historical filters: use snapshot unit name
└── Current filters: map via unit_mapping table
```

#### **3. Query Optimization**
- Partition tables by `submitted_at` (monthly)
- Composite index: `(tenant_id, survey_id, submitted_at)`
- Materialized views refresh: hourly for active surveys

#### **4. Filter Resolution Logic**

```sql
-- User selects: "Department = Sales"

-- IF filterMode = HISTORICAL:
SELECT * FROM survey_responses
WHERE snapshot_core->>'department' = 'Sales'

-- IF filterMode = CURRENT:
-- Step 1: Find current unit_id(s) named 'Sales'
-- Step 2: Map to historical unit_id(s) via unit_mapping
-- Step 3: Filter responses
SELECT * FROM survey_responses
WHERE snapshot_core->>'unit_id' IN (mapped_unit_ids)
```

---

## E - Evaluation

### **Bottlenecks Identified**

1. **Temporal joins** - Slow for complex queries
   - **Mitigation:** Snapshot core attributes
   
2. **Snapshot storage** - 20GB/year growth
   - **Mitigation:** Compress JSONB, archive old surveys
   
3. **Filter translation** - Unit mapping complexity
   - **Mitigation:** Cache mapping table, denormalize hierarchy

### **Trade-offs Analysis**

| Aspect | Choice | Trade-off |
|--------|--------|-----------|
| Storage | Hybrid (20KB/response) | 2x storage for 50% faster queries |
| Accuracy | Immutable snapshots | Can't fix bugs retroactively |
| Flexibility | Partial snapshot | Complex queries need joins |
| Consistency | Eventual (materialized views) | 1-hour lag acceptable for analytics |

---

## D - Distinctive Component/Feature

### **The Unique Complexity: Temporal Filter Translation**

This is the hardest part of the system - not the snapshotting itself, but **mapping current org structure to historical responses**.

#### **Example Scenario**

```
Timeline:
- Time T0: "Sales APAC" (id=123) exists
- Time T1: Response submitted by employee in unit 123
- Time T2: "Sales APAC" renamed to "Revenue APAC" (still id=123)
- Time T3: "Revenue APAC" split into "SEA Sales" (id=456) + "ANZ Sales" (id=457)
- Time T4: User queries dashboard filtering "SEA Sales"

Question: Should the T1 response appear in results?
Answer: Depends on filter mode!
```


## Questions to be Addressed

### 1. Data Snapshot Strategy

#### How would you design the system to snapshot respondent-level data?

**Design:**

We implement a **two-tier snapshot strategy**:

**Tier 1: Core Snapshot (Inline JSONB)**
```go
type ResponseSnapshot struct {
    ResponseID   string    `json:"response_id"`
    EmployeeID   string    `json:"employee_id"`
    SubmittedAt  time.Time `json:"submitted_at"`
    SnapshotCore map[string]interface{} `json:"snapshot_core"` // JSONB in DB
}
```

The `snapshot_core` contains 15-20 critical attributes:
- `employee_name`
- `department` / `unit_id` / `unit_name`
- `performance_grade`
- `age` / `tenure`
- `role` / `job_level`
- `location` / `region`
- Direct manager (if frequently filtered)

**Tier 2: Extended Attributes (Referenced)**
```go
type EmployeeHistory struct {
    EmployeeID     string    `db:"employee_id"`
    AttributeType  string    `db:"attribute_type"`
    AttributeValue string    `db:"attribute_value"`
    ValidFrom      time.Time `db:"valid_from"`
    ValidTo        time.Time `db:"valid_to"`
    VersionID      string    `db:"version_id"`
}
```

Extended attributes (training status, certifications, custom fields) are stored with temporal validity periods.

**Snapshot Capture Process:**

```
1. Response Submission Event Received
2. BEGIN TRANSACTION
3. Query current employee state (from live employee table)
4. Extract core attributes → serialize to JSONB
5. Generate version_id for this point in time
6. Store response with snapshot_core + version_id
7. COMMIT TRANSACTION
```

**Code Implementation:**
```go
func (s *SnapshotService) CaptureSnapshot(ctx context.Context, employeeID string, timestamp time.Time) (*Snapshot, error) {
    // Get current employee state
    employee, err := s.employeeRepo.GetByID(ctx, employeeID)
    if err != nil {
        return nil, err
    }
    
    // Get org unit at this time
    orgUnit, err := s.orgRepo.GetUnitAtTime(ctx, employee.UnitID, timestamp)
    if err != nil {
        return nil, err
    }
    
    // Build core snapshot
    snapshotCore := map[string]interface{}{
        "employee_name": employee.Name,
        "department": orgUnit.Name,
        "unit_id": orgUnit.ID,
        "grade": employee.PerformanceGrade,
        "age": calculateAge(employee.BirthDate, timestamp),
        "tenure": calculateTenure(employee.HireDate, timestamp),
        "role": employee.Role,
    }
    
    // Generate version ID
    versionID := generateVersionID(employeeID, timestamp)
    
    return &Snapshot{
        SnapshotCore: snapshotCore,
        VersionID: versionID,
        Timestamp: timestamp,
    }, nil
}
```

#### How would you ensure the dashboard can use both real-time and snapshot data?

**Implementation Strategy:**

We provide a **filter mode parameter** in all dashboard queries:

```go
type FilterMode string

const (
    FilterModeHistorical FilterMode = "HISTORICAL" // Data as-of response time
    FilterModeCurrent    FilterMode = "CURRENT"    // Map to current org structure
    FilterModeHybrid     FilterMode = "HYBRID"     // Show both with breakdown
)

type DashboardQuery struct {
    Filters    map[string]interface{} `json:"filters"`
    FilterMode FilterMode             `json:"filter_mode"`
    TimeRange  TimeRange              `json:"time_range"`
}
```

**Query Resolution:**

```go
func (s *DashboardService) QueryResponses(ctx context.Context, query DashboardQuery) ([]Response, error) {
    switch query.FilterMode {
    case FilterModeHistorical:
        // Query snapshot_core directly
        return s.queryHistoricalSnapshot(ctx, query)
        
    case FilterModeCurrent:
        // Translate current org structure to historical unit IDs
        historicalUnitIDs, err := s.orgMapper.MapCurrentToHistorical(
            ctx, 
            query.Filters["department"],
            query.TimeRange,
        )
        if err != nil {
            return nil, err
        }
        query.Filters["unit_id"] = historicalUnitIDs
        return s.queryHistoricalSnapshot(ctx, query)
        
    case FilterModeHybrid:
        // Execute both and merge results with provenance
        historical := s.queryHistoricalSnapshot(ctx, query)
        current := s.queryCurrentMapped(ctx, query)
        return s.mergeWithProvenance(historical, current)
    }
}
```

**Real-time data use cases:**
1. **Employee notifications**: Use current email/department for routing
2. **Manager dashboards**: Show current team structure
3. **Compliance reports**: Use current organizational hierarchy

**Snapshot data use cases:**
1. **Survey analytics**: Analyze responses in historical context
2. **Trend analysis**: Compare departments as they were across different time periods
3. **Audit trails**: Prove data accuracy at time of collection

---

### 2. Impact of Organizational Changes

#### How would you ensure data consistency if a unit is renamed or restructured?

**Solution: Organizational Mapping Graph**

We maintain a separate table that tracks organizational lineage:

```go
type OrgUnitMapping struct {
    SourceUnitID     string           `db:"source_unit_id"`
    TargetUnitIDs    []string         `db:"target_unit_ids"` // Array for splits
    RelationshipType MappingType      `db:"relationship_type"`
    EffectiveDate    time.Time        `db:"effective_date"`
    Description      string           `db:"description"`
}

type MappingType string
const (
    MappingTypeRename MappingType = "RENAME" // 1:1 (same unit, new name)
    MappingTypeMerge  MappingType = "MERGE"  // N:1 (multiple → one)
    MappingTypeSplit  MappingType = "SPLIT"  // 1:N (one → multiple)
)
```

**Database Schema:**
```sql
CREATE TABLE org_unit_mapping (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_unit_id VARCHAR(255) NOT NULL,
    target_unit_ids TEXT[] NOT NULL, -- PostgreSQL array
    relationship_type VARCHAR(50) NOT NULL CHECK (relationship_type IN ('RENAME', 'MERGE', 'SPLIT')),
    effective_date TIMESTAMP NOT NULL,
    description TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_org_mapping_source ON org_unit_mapping(source_unit_id, effective_date);
CREATE INDEX idx_org_mapping_target ON org_unit_mapping USING GIN(target_unit_ids);
```

**Example Scenarios:**

**Scenario 1: Rename**
```
"Sales APAC" (id=123) → "Revenue APAC" (id=123)

Mapping:
{
    source_unit_id: "123",
    target_unit_ids: ["123"],
    relationship_type: "RENAME",
    effective_date: "2024-06-01",
    description: "Rebranded to Revenue organization"
}
```

**Scenario 2: Split**
```
"Revenue APAC" (id=123) → ["SEA Sales" (id=456), "ANZ Sales" (id=457)]

Mapping:
{
    source_unit_id: "123",
    target_unit_ids: ["456", "457"],
    relationship_type: "SPLIT",
    effective_date: "2024-09-01",
    description: "Split into regional teams"
}
```

**Scenario 3: Merge**
```
["Product A" (id=100), "Product B" (id=101)] → "Product Division" (id=200)

Mappings (two records):
{
    source_unit_id: "100",
    target_unit_ids: ["200"],
    relationship_type: "MERGE",
    effective_date: "2024-03-01"
},
{
    source_unit_id: "101",
    target_unit_ids: ["200"],
    relationship_type: "MERGE",
    effective_date: "2024-03-01"
}
```

**Data Consistency Guarantees:**

1. **Snapshot immutability**: Response snapshots never change
2. **Org history versioning**: `org_units_history` table tracks all name/structure changes
3. **Mapping integrity**: Foreign key constraints ensure referenced units exist
4. **Transaction atomicity**: Org changes + mapping creation in single transaction

#### How would you handle historical attribution when units no longer exist?

**Strategy: Preserve + Annotate**

```go
type OrgUnit struct {
    UnitID       string    `db:"unit_id"`
    UnitName     string    `db:"unit_name"`
    ParentUnitID *string   `db:"parent_unit_id"`
    ValidFrom    time.Time `db:"valid_from"`
    ValidTo      time.Time `db:"valid_to"` // NULL = currently active
    IsActive     bool      `db:"is_active"`
}
```

**When a unit is dissolved:**

1. **Don't delete** - Set `valid_to` timestamp and `is_active = false`
2. **Create mapping** to successor unit(s)
3. **Dashboard display** shows "(Historical)" tag

```go
func (s *OrgService) DisplayUnitName(unitID string, asOfDate time.Time) string {
    unit, err := s.repo.GetUnitAtTime(ctx, unitID, asOfDate)
    if err != nil {
        return "Unknown Unit"
    }
    
    // Check if unit still exists
    if unit.ValidTo.Before(time.Now()) {
        return fmt.Sprintf("%s (Historical)", unit.UnitName)
    }
    
    return unit.UnitName
}
```

**Dashboard UI Example:**
```
Filter: Department
Options:
  ☐ Sales APAC (Historical - now part of Revenue APAC)
  ☐ Revenue APAC (Current)
  ☐ Engineering (Current)
```

**Query Implementation:**
```sql
-- Get all responses from a historical unit
SELECT r.* 
FROM survey_responses r
WHERE r.snapshot_core->>'unit_id' = '123'  -- Historical unit

-- Show what it became
SELECT m.target_unit_ids, u.unit_name
FROM org_unit_mapping m
JOIN org_units_history u ON u.unit_id = ANY(m.target_unit_ids)
WHERE m.source_unit_id = '123'
  AND u.valid_to IS NULL  -- Current state
```

#### How would you handle cases where users want to filter by current structure vs. historical?

**Implementation: Filter Mode with Translation Layer**

```go
type FilterResolver struct {
    orgMapper *OrgMapper
    cache     *MappingCache
}

func (fr *FilterResolver) ResolveFilter(ctx context.Context, filter Filter, mode FilterMode) (ResolvedFilter, error) {
    if mode == FilterModeHistorical {
        // Direct query on snapshot_core
        return ResolvedFilter{
            Field: "snapshot_core->>" + filter.Field,
            Value: filter.Value,
        }, nil
    }
    
    if mode == FilterModeCurrent {
        // User selected "Revenue APAC" (current name)
        // Need to find ALL historical unit IDs that map to it
        
        currentUnitID := fr.lookupCurrentUnitID(ctx, filter.Value)
        historicalUnitIDs := fr.orgMapper.MapCurrentToHistorical(ctx, currentUnitID)
        
        return ResolvedFilter{
            Field: "snapshot_core->>'unit_id'",
            Values: historicalUnitIDs, // IN clause
        }, nil
    }
}
```

**Mapping Algorithm (Backward Traversal):**

```go
func (om *OrgMapper) MapCurrentToHistorical(ctx context.Context, currentUnitID string) ([]string, error) {
    visited := make(map[string]bool)
    result := []string{currentUnitID}
    queue := []string{currentUnitID}
    
    for len(queue) > 0 {
        unitID := queue[0]
        queue = queue[1:]
        
        if visited[unitID] {
            continue
        }
        visited[unitID] = true
        
        // Find mappings where this unit is a target
        mappings, err := om.repo.FindMappingsByTarget(ctx, unitID)
        if err != nil {
            return nil, err
        }
        
        for _, mapping := range mappings {
            // Add source unit (predecessor)
            if !visited[mapping.SourceUnitID] {
                result = append(result, mapping.SourceUnitID)
                queue = append(queue, mapping.SourceUnitID)
            }
        }
    }
    
    return result, nil
}
```

**Example: User selects "Revenue APAC" in current mode**

```
Graph traversal:
Revenue APAC (456) 
  ← came from Revenue APAC (123) via RENAME
  ← came from Sales APAC (123) via RENAME

Result: Include responses where unit_id IN (456, 123)
```

**SQL Query Generated:**
```sql
SELECT * FROM survey_responses
WHERE snapshot_core->>'unit_id' IN ('456', '123')
  AND submitted_at BETWEEN $1 AND $2
```

---

### 3. Dashboard Filtering Logic

#### How should filters behave when employee attributes can change?

**Design Principle: Time-Aware Filtering**

All filters implicitly include temporal context:

```go
type Filter struct {
    Field      string      `json:"field"`
    Operator   Operator    `json:"operator"`
    Value      interface{} `json:"value"`
    AsOfDate   *time.Time  `json:"as_of_date,omitempty"` // Optional: override with specific date
}

type Operator string
const (
    OpEqual        Operator = "eq"
    OpIn           Operator = "in"
    OpGreaterThan  Operator = "gt"
    OpLessThan     Operator = "lt"
    OpBetween      Operator = "between"
)
```

**Filter Behavior by Attribute Type:**

| Attribute Type | Storage Location | Filter Behavior |
|----------------|------------------|-----------------|
| Department, Grade, Role | `snapshot_core` | Direct JSONB query |
| Age, Tenure | `snapshot_core` (calculated) | Stored as value at response time |
| Training Status, Certifications | `employee_history` | Temporal join with `valid_from`/`valid_to` |
| Custom Fields | `employee_history` | Same as above |

**Age/Tenure Special Handling:**

```go
// Option 1: Store calculated values (RECOMMENDED for performance)
snapshotCore["age"] = calculateAge(employee.BirthDate, responseTime)
snapshotCore["tenure"] = calculateTenure(employee.HireDate, responseTime)

// Option 2: Store birthdate/hire_date, calculate at query time
snapshotCore["birth_date"] = employee.BirthDate
snapshotCore["hire_date"] = employee.HireDate

// Then query:
// WHERE EXTRACT(YEAR FROM AGE(submitted_at, snapshot_core->>'birth_date'::date)) = 30
```

We chose **Option 1** for performance - pre-calculate and store.

**Filter Examples:**

```go
// Filter 1: Simple core attribute
{
    "field": "performance_grade",
    "operator": "eq",
    "value": "A"
}
// SQL: WHERE snapshot_core->>'performance_grade' = 'A'

// Filter 2: Tenure range
{
    "field": "tenure",
    "operator": "between",
    "value": [3, 5]
}
// SQL: WHERE (snapshot_core->>'tenure')::int BETWEEN 3 AND 5

// Filter 3: Extended attribute (training)
{
    "field": "leadership_training",
    "operator": "eq",
    "value": "completed"
}
// SQL: Requires JOIN to employee_history
// WHERE eh.attribute_type = 'leadership_training'
//   AND eh.attribute_value = 'completed'
//   AND r.submitted_at BETWEEN eh.valid_from AND eh.valid_to
```

#### How would you support filtering based on current vs. historical org structure?

**Implementation: Three-Mode Filter System**

```go
func (ds *DashboardService) BuildQuery(ctx context.Context, req DashboardQueryRequest) (*QueryBuilder, error) {
    qb := NewQueryBuilder("survey_responses")
    
    for _, filter := range req.Filters {
        if isOrgStructureFilter(filter.Field) {
            // Special handling for org filters
            switch req.FilterMode {
            case FilterModeHistorical:
                qb.AddJSONBFilter("snapshot_core", filter.Field, filter.Value)
                
            case FilterModeCurrent:
                // Translate current unit name → historical unit IDs
                mappedIDs, err := ds.resolveCurrentToHistorical(ctx, filter.Value)
                if err != nil {
                    return nil, err
                }
                qb.AddJSONBFilter("snapshot_core", "unit_id", mappedIDs)
                
            case FilterModeHybrid:
                // Include both with provenance tracking
                qb.AddHybridFilter(filter)
            }
        } else {
            // Regular attribute filter
            qb.AddJSONBFilter("snapshot_core", filter.Field, filter.Value)
        }
    }
    
    return qb, nil
}
```

**UI/UX Design:**

```
┌─────────────────────────────────────────────┐
│ Dashboard Filters                            │
├─────────────────────────────────────────────┤
│                                              │
│ View Mode: ⦿ Historical  ○ Current  ○ Both  │
│                                              │
│ Department: [Revenue APAC ▼]                │
│                                              │
│ ℹ️  Historical Mode: Filters by department  │
│    names as they were when responses were   │
│    submitted.                                │
│                                              │
│ Performance Grade: [A ▼]                    │
│ Tenure: [3-5 years]                         │
│                                              │
└─────────────────────────────────────────────┘
```

**Result Display with Provenance:**

```
Showing 1,247 responses

Filter Mode: Current
Department: "Revenue APAC"

Note: Includes historical responses from:
  • Sales APAC (Jan 2024 - May 2024) - 523 responses
  • Revenue APAC (Jun 2024 - Present) - 724 responses
```

---

### 4. Data Integrity vs. Storage Tradeoffs

#### What are the tradeoffs between snapshotting vs. querying live tables?

**Comparison Matrix:**

| Approach | Storage per Response | Query Performance | Data Integrity | Flexibility | Complexity |
|----------|---------------------|-------------------|----------------|-------------|------------|
| **Full Snapshot** | 50KB (500 attrs) | Excellent (1x) | Perfect (immutable) | Low (can't fix bugs) | Low |
| **No Snapshot (Temporal Only)** | 2KB (just IDs) | Poor (5-10x slower) | Good (can fix data) | High (query anything) | High |
| **Hybrid (Our Choice)** | 20KB (20 core attrs) | Good (1.5-2x) | Very Good | Medium-High | Medium |

**Detailed Analysis:**

**Full Snapshot Approach:**
```go
// Store everything
snapshotFull := map[string]interface{}{
    // 500 attributes...
}
// Size: ~50KB per response
// 1M responses = 50GB
```

**Pros:**
- ✅ Blazing fast queries (no JOINs)
- ✅ Perfect point-in-time accuracy
- ✅ Simple query logic

**Cons:**
- ❌ Massive storage overhead (50GB/year)
- ❌ Can't fix data errors retroactively
- ❌ Schema evolution difficult (adding new attributes)
- ❌ Wasteful for rarely-used attributes

**Temporal Tables Only:**
```go
// Store minimal data
type Response struct {
    ResponseID  string
    EmployeeID  string
    SubmittedAt time.Time
    VersionID   string // Points to employee_history
}
// Size: ~2KB per response
// 1M responses = 2GB
```

**Pros:**
- ✅ Minimal storage (2GB/year)
- ✅ Can fix historical data bugs
- ✅ Easy schema evolution

**Cons:**
- ❌ Slow queries (3-5 table JOINs)
- ❌ Complex query logic
- ❌ Difficult to optimize indexes
- ❌ Risk of temporal join bugs

**Hybrid Approach (Recommended):**
```go
type Response struct {
    ResponseID   string
    EmployeeID   string
    SubmittedAt  time.Time
    SnapshotCore map[string]interface{} // 20 core attributes
    VersionID    string // Reference to extended attributes
}
// Size: ~20KB per response
// 1M responses = 20GB
```

**Pros:**
- ✅ Fast for 80% of queries (no JOIN)
- ✅ Flexible for complex queries (JOIN when needed)
- ✅ Reasonable storage (20GB/year)
- ✅ Balance between performance and flexibility

**Cons:**
- ⚠️ Need to choose core attributes carefully
- ⚠️ Moderate complexity

#### How would you scale this across millions of records?

**Scalability Strategy:**

**1. Database Partitioning**
```sql
-- Partition by submission time (monthly)
CREATE TABLE survey_responses_2024_01 PARTITION OF survey_responses
    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');

CREATE TABLE survey_responses_2024_02 PARTITION OF survey_responses
    FOR VALUES FROM ('2024-02-01') TO ('2024-03-01');

-- Partition by tenant_id for multi-tenancy
CREATE TABLE survey_responses_tenant_001 PARTITION OF survey_responses
    FOR VALUES IN ('tenant_001');
```

**Benefits:**
- Query performance: Only scan relevant partitions
- Archival: Drop old partitions easily
- Backup/Restore: Per-partition operations

**2. Indexing Strategy**
```sql
-- Composite index for common queries
CREATE INDEX idx_responses_tenant_survey_time 
    ON survey_responses (tenant_id, survey_id, submitted_at DESC);

-- GIN index for JSONB queries
CREATE INDEX idx_responses_snapshot_core 
    ON survey_responses USING GIN (snapshot_core);

-- Partial index for active surveys
CREATE INDEX idx_responses_active 
    ON survey_responses (survey_id, submitted_at)
    WHERE survey_id IN (SELECT id FROM surveys WHERE status = 'ACTIVE');
```

**3. Materialized Views for Aggregations**
```sql
-- Pre-aggregate common dashboard queries
CREATE MATERIALIZED VIEW mv_department_scores AS
SELECT 
    snapshot_core->>'department' as department,
    survey_id,
    DATE_TRUNC('month', submitted_at) as month,
    COUNT(*) as response_count,
    AVG((snapshot_core->>'engagement_score')::numeric) as avg_score
FROM survey_responses
GROUP BY 1, 2, 3;

CREATE INDEX ON mv_department_scores (department, survey_id, month);

-- Refresh strategy
REFRESH MATERIALIZED VIEW CONCURRENTLY mv_department_scores;
```

**4. Caching Layer**
```go
type CacheLayer struct {
    redis *redis.Client
    ttl   time.Duration
}

func (c *CacheLayer) GetDashboardData(ctx context.Context, cacheKey string) (*DashboardData, error) {
    // Check cache first
    cached, err := c.redis.Get(ctx, cacheKey).Bytes()
    if err == nil {
        var data DashboardData
        json.Unmarshal(cached, &data)
        return &data, nil
    }
    
    // Cache miss - query database
    data, err := c.queryDatabase(ctx, cacheKey)
    if err != nil {
        return nil, err
    }
    
    // Store in cache
    c.redis.Set(ctx, cacheKey, data, c.ttl)
    return data, nil
}
```

**5. Read Replicas**
```
┌─────────────┐
│   Primary   │  (Writes: response submissions)
│  PostgreSQL │
└──────┬──────┘
       │
       ├─────► Replica 1 (Dashboard queries - Region A)
       ├─────► Replica 2 (Dashboard queries - Region B)
       └─────► Replica 3 (Reporting/Analytics)
```

**6. Archive Strategy**
```go
// Archive responses older than 2 years to cold storage
func (s *ArchivalService) ArchiveOldResponses(ctx context.Context, cutoffDate time.Time) error {
    // 1. Export to S3/GCS
    responses, err := s.repo.GetResponsesBefore(ctx, cutoffDate)
    if err != nil {
        return err
    }
    
    err = s.exportToObjectStorage(ctx, responses)
    if err != nil {
        return err
    }
    
    // 2. Delete from hot storage
    return s.repo.DeleteResponsesBefore(ctx, cutoffDate)
}
```

**Performance Targets:**

| Metric | Target | Strategy |
|--------|--------|----------|
| Dashboard load time | < 2s (p95) | Materialized views + caching |
| Response submission | < 200ms | Async snapshot capture |
| Concurrent users | 1000+ | Read replicas + connection pooling |
| Data volume | 10M responses | Partitioning + archival |
| Query complexity | Up to 10 filters | Optimized indexes + query planner |

**Monitoring:**
```go
// Track query performance
type QueryMetrics struct {
    QueryType     string
    Duration      time.Duration
    RowsScanned   int64
    IndexUsed     bool
    CacheHit      bool
}

func (m *MetricsCollector) RecordQuery(ctx context.Context, metrics QueryMetrics) {
    // Send to monitoring system (Prometheus/Datadog)
    m.prometheus.ObserveQueryDuration(metrics.QueryType, metrics.Duration)
    m.prometheus.IncrementQueryCount(metrics.QueryType)
    
    if !metrics.IndexUsed {
        m.logger.Warn("Query without index", "query_type", metrics.QueryType)
    }
}
```

---

### 5. Cross-Team Collaboration

#### What assumptions/contracts would you expect from other teams?

**1. HR/Employee Data Team**

**Contracts Required:**

```go
// Employee data must be available via API
type EmployeeDataContract interface {
    // Get current employee state
    GetEmployee(ctx context.Context, employeeID string) (*Employee, error)
    
    // Get employee state at specific time
    GetEmployeeAtTime(ctx context.Context, employeeID string, timestamp time.Time) (*Employee, error)
    
    // Subscribe to employee change events
    SubscribeToChanges(ctx context.Context, handler EmployeeChangeHandler) error
}

type EmployeeChangeEvent struct {
    EmployeeID    string
    ChangeType    string // "DEPARTMENT_CHANGE", "PROMOTION", "TERMINATION"
    OldValue      interface{}
    NewValue      interface{}
    EffectiveDate time.Time
}
```

**Assumptions:**
- ✅ Employee IDs are stable (never change)
- ✅ Employee data is eventually consistent (max 5 min lag)
- ✅ Change events are published for all attribute updates
- ✅ Historical data is retained for at least 7 years (compliance)

**SLA Expected:**
- API availability: 99.9%
- API response time: < 100ms (p95)
- Event delivery: At-least-once guarantee within 1 minute

**2. Organization/HR Structure Team**

**Contracts Required:**

```go
type OrgStructureContract interface {
    // Get org hierarchy at specific time
    GetOrgHierarchy(ctx context.Context, asOf time.Time) (*OrgTree, error)
    
    // Get unit details
    GetUnit(ctx context.Context, unitID string) (*OrgUnit, error)
    
    // Notification before org changes (24-hour notice)
    NotifyUpcomingRestructure(ctx context.Context, restructure RestructureEvent) error
}

type RestructureEvent struct {
    ChangeType     string // "RENAME", "MERGE", "SPLIT", "DISSOLVE"
    AffectedUnits  []string
    EffectiveDate  time.Time
    NewStructure   *OrgTree
    Description    string
}
```

**Assumptions:**
- ✅ 24-hour advance notice for org restructures
- ✅ Org changes are effective at 00:00 UTC on specified date
- ✅ Unit IDs for renamed units remain the same
- ✅ Dissolved units are marked inactive, not deleted

**3. Survey Team**

**Contracts Required:**

```go
type SurveyContract interface {
    // Survey lifecycle events
    OnSurveyLaunched(ctx context.Context, surveyID string) error
    OnSurveyCompleted(ctx context.Context, surveyID string) error
    
    // Response submission event
    OnResponseSubmitted(ctx context.Context, event ResponseEvent) error
}

type ResponseEvent struct {
    ResponseID  string
    SurveyID    string
    EmployeeID  string
    SubmittedAt time.Time
    Answers     map[string]interface{}
}
```

**Assumptions:**
- ✅ Response submission timestamp is authoritative (database server time)
- ✅ Response IDs are unique and immutable
- ✅ Responses cannot be edited after submission
- ✅ Survey configuration defines which attributes to snapshot

**4. Platform/Infrastructure Team**

**Contracts Required:**

```go
type PlatformContract interface {
    // Multi-tenancy
    GetTenantConfig(ctx context.Context, tenantID string) (*TenantConfig, error)
    
    // Time synchronization
    GetServerTime(ctx context.Context) (time.Time, error)
    
    // Database backup/restore
    BackupDatabase(ctx context.Context, timestamp time.Time) error
    RestoreDatabase(ctx context.Context, backupID string) error
}
```

**Assumptions:**
- ✅ Database server time is single source of truth (UTC)
- ✅ All services use NTP for time sync (max 1s skew)
- ✅ Tenant data is logically isolated (row-level security)
- ✅ Database backups: daily + point-in-time recovery (7 days)

**5. API Gateway/Security Team**

**Contracts Required:**

```go
type SecurityContract interface {
    // Authentication
    ValidateToken(ctx context.Context, token string) (*UserClaims, error)
    
    // Authorization
    CheckPermission(ctx context.Context, userID, resource, action string) (bool, error)
    
    // Audit logging
    LogDataAccess(ctx context.Context, event AuditEvent) error
}
```

**Assumptions:**
- ✅ JWT tokens include tenant_id claim
- ✅ Rate limiting: 100 req/s per tenant
- ✅ All data access is logged for compliance
- ✅ GDPR/data retention policies enforced

**Inter-Team Communication:**

```
┌─────────────────────────────────────────────┐
│         Snapshot Service (Our Team)          │
└───────────┬─────────────────────────────────┘
            │
    ┌───────┼───────┬───────────┬──────────┐
    │       │       │           │          │
┌───▼───┐ ┌─▼──┐ ┌─▼────┐ ┌────▼─────┐ ┌─▼────┐
│  HR   │ │Org │ │Survey│ │Platform  │ │  API │
│ Data  │ │Team│ │Team  │ │ Team     │ │Gateway│
└───────┘ └────┘ └──────┘ └──────────┘ └──────┘
```

**Communication Protocols:**

1. **Synchronous**: REST APIs for reads (employee data, org structure)
2. **Asynchronous**: Event streams for changes (Kafka/SQS)
3. **Documentation**: OpenAPI specs + AsyncAPI specs
4. **Monitoring**: Shared metrics dashboard (Datadog/Grafana)

---

### 6. Technical Challenges

#### Challenge 1: Time Synchronization

**Problem:**
In distributed systems, clock skew between application servers can cause:
- Response timestamp ≠ actual submission time
- Race conditions when querying employee state
- Inconsistent snapshots

**Solution:**
```go
// Always use database server time
func (r *ResponseRepository) CreateResponse(ctx context.Context, resp *Response) error {
    query := `
        INSERT INTO survey_responses (
            response_id, employee_id, submitted_at, snapshot_core
        ) VALUES ($1, $2, NOW(), $3)  -- NOW() is DB server time
        RETURNING submitted_at
    `
    
    err := r.db.QueryRow(query, 
        resp.ResponseID, 
        resp.EmployeeID, 
        resp.SnapshotCore,
    ).Scan(&resp.SubmittedAt)
    
    return err
}
```

**Alternative: Hybrid Logical Clocks**
```go
type HLC struct {
    physicalTime int64
    logicalTime  int64
}

func (h *HLC) Now() int64 {
    physical := time.Now().UnixNano()
    if physical > h.physicalTime {
        h.physicalTime = physical
        h.logicalTime = 0
    } else {
        h.logicalTime++
    }
    return h.physicalTime<<32 | h.logicalTime
}
```

#### Challenge 2: Snapshot Transaction Isolation

**Problem:**
Employee attributes might be mid-update when snapshot is captured:
```
T1: BEGIN UPDATE employees SET department='Sales'
T2: Snapshot captures → sees old department
T3: COMMIT
T4: Snapshot captures → sees new department
```

**Solution: Consistent Snapshot Reads**
```go
func (s *SnapshotService) CaptureSnapshot(ctx context.Context, employeeID string) error {
    tx, err := s.db.BeginTx(ctx, &sql.TxOptions{
        Isolation: sql.LevelRepeatableRead, // Ensures consistent view
    })
    if err != nil {
        return err
    }
    defer tx.Rollback()
    
    // All reads in this transaction see same snapshot
    employee, _ := s.getEmployee(tx, employeeID)
    orgUnit, _ := s.getOrgUnit(tx, employee.UnitID)
    
    // Create snapshot
    snapshot := s.buildSnapshot(employee, orgUnit)
    
    // Store
    _, err = s.saveSnapshot(tx, snapshot)
    if err != nil {
        return err
    }
    
    return tx.Commit()
}
```

#### Challenge 3: JSONB Query Performance

**Problem:**
JSONB queries can be slow without proper indexes:
```sql
-- Slow: Full table scan
SELECT * FROM survey_responses
WHERE snapshot_core->>'department' = 'Sales';
```

**Solution: GIN Indexes + Expression Indexes**
```sql
-- GIN index for all JSONB keys
CREATE INDEX idx_snapshot_gin ON survey_responses USING GIN (snapshot_core);

-- Expression index for specific fields
CREATE INDEX idx_snapshot_department 
    ON survey_responses ((snapshot_core->>'department'));

-- Now fast
EXPLAIN ANALYZE
SELECT * FROM survey_responses
WHERE snapshot_core->>'department' = 'Sales';
-- Index Scan using idx_snapshot_department
```

#### Challenge 4: Multi-Tenant Data Isolation

**Problem:**
Accidental cross-tenant data leaks in queries.

**Solution: Row-Level Security (RLS)**
```sql
-- Enable RLS
ALTER TABLE survey_responses ENABLE ROW LEVEL SECURITY;

-- Policy: Users can only see their tenant's data
CREATE POLICY tenant_isolation ON survey_responses
    USING (tenant_id = current_setting('app.tenant_id')::uuid);

-- In application
func (s *Service) SetTenant(ctx context.Context, tenantID string) error {
    _, err := s.db.Exec(ctx, "SET app.tenant_id = $1", tenantID)
    return err
}
```

#### Challenge 5: Circular Org Structure References

**Problem:**
During restructures, temporary circular references:
```
Unit A → parent is Unit B
Unit B → parent is Unit A (error during migration)
```

**Solution: Validation + Materialized Path**
```go
func (s *OrgService) ValidateHierarchy(ctx context.Context, orgTree *OrgTree) error {
    visited := make(map[string]bool)
    
    var checkCycle func(unitID string, path map[string]bool) error
    checkCycle = func(unitID string, path map[string]bool) error {
        if path[unitID] {
            return fmt.Errorf("circular reference detected: %v", path)
        }
        
        path[unitID] = true
        unit := orgTree.FindUnit(unitID)
        
        if unit.ParentUnitID != nil {
            return checkCycle(*unit.ParentUnitID, path)
        }
        
        return nil
    }
    
    for _, unit := range orgTree.Units {
        if err := checkCycle(unit.UnitID, make(map[string]bool)); err != nil {
            return err
        }
    }
    
    return nil
}

// Store materialized path for fast queries
type OrgUnit struct {
    UnitID   string
    Path     string // "root.emea.sales.team1" (ltree in PostgreSQL)
}

// CREATE INDEX ON org_units USING GIST (path);
// SELECT * FROM org_units WHERE path <@ 'root.emea';
```

#### Challenge 6: Snapshot Bug Fixes

**Problem:**
Bug in snapshot logic affected 10,000 responses. How to fix?

**Solution: Audit Log + Reprocessing Pipeline**
```go
// Store raw event alongside snapshot
type ResponseAudit struct {
    ResponseID     string
    RawEvent       json.RawMessage // Original event
    SnapshotV1     json.RawMessage // Snapshot created
    SnapshotLogic  string          // Version: "v1.2.3"
    CreatedAt      time.Time
}

// Reprocessing
func (s *SnapshotService) ReprocessResponses(ctx context.Context, responseIDs []string) error {
    for _, id := range responseIDs {
        // Get original event
        audit, err := s.auditRepo.Get(ctx, id)
        if err != nil {
            return err
        }
        
        // Reprocess with fixed logic
        newSnapshot, err := s.captureSnapshotV2(audit.RawEvent)
        if err != nil {
            return err
        }
        
        // Update (with versioning)
        return s.updateSnapshot(ctx, id, newSnapshot, "v1.2.4")
    }
}
```
