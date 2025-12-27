package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"dashboard-case-study/pkg/models"
	"dashboard-case-study/pkg/repository"
	"dashboard-case-study/pkg/service"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

func main() {
	// Database connection
	dbURL := "postgres://postgres:postgres@localhost:5432/snapshot_poc?sslmode=disable"
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Ping database
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("✓ Connected to database")

	// Initialize repositories
	employeeRepo := repository.NewPostgresEmployeeRepository(db)
	orgRepo := repository.NewPostgresOrgRepository(db)
	responseRepo := repository.NewPostgresResponseRepository(db)

	// Initialize services
	snapshotSvc := service.NewSnapshotService(employeeRepo, orgRepo)
	responseSvc := service.NewResponseService(responseRepo, snapshotSvc)
	dashboardSvc := service.NewDashboardService(responseRepo, orgRepo)

	// Setup router
	r := mux.NewRouter()

	// Health check
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"status": "healthy",
			"time":   time.Now().Format(time.RFC3339),
		})
	}).Methods("GET")

	// Submit response endpoint
	r.HandleFunc("/api/v1/surveys/{surveyId}/responses", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		surveyID := vars["surveyId"]

		var req models.SubmitResponseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Submit response (tenant_id would come from JWT in production)
		response, err := responseSvc.Submit(r.Context(), surveyID, req.EmployeeID, "tenant_demo", req.Answers)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to submit response: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models.SubmitResponseResponse{
			ResponseID:  response.ResponseID,
			SubmittedAt: response.SubmittedAt,
		})
	}).Methods("POST")

	// Query dashboard endpoint
	r.HandleFunc("/api/v1/dashboards/query", func(w http.ResponseWriter, r *http.Request) {
		var query models.DashboardQuery
		if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Execute query
		result, err := dashboardSvc.Query(r.Context(), query)
		if err != nil {
			http.Error(w, fmt.Sprintf("Query failed: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}).Methods("POST")

	// Start server
	port := ":8080"
	log.Printf("🚀 Server starting on http://localhost%s", port)
	log.Printf("   Health: http://localhost%s/health", port)
	log.Fatal(http.ListenAndServe(port, r))
}
