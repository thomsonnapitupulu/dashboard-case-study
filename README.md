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

--- WORKING IN PROGRESS ---