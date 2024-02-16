package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/theheadmen/goDipl2/internal/dbconnector"
	"github.com/theheadmen/goDipl2/internal/models"
	"github.com/theheadmen/goDipl2/internal/server"
)

type Config struct {
	Host     string
	Port     uint16
	Username string
	Password string
	DBName   string
}

// TestLoyaltySystemWithTestContainer tests the LoyaltySystem with a disposable PostgreSQL container
func TestLoyaltySystemWithTestContainer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	cfg := &Config{
		Username: "postgres",
		Password: "example",
		DBName:   "godb",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	postgresContainer, err := tcpostgres.RunContainer(ctx,
		testcontainers.WithImage("docker.io/postgres:latest"),
		tcpostgres.WithDatabase(cfg.DBName),
		tcpostgres.WithUsername(cfg.Username),
		tcpostgres.WithPassword(cfg.Password),
		tcpostgres.WithInitScripts(),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(5*time.Second),
		),
	)

	require.NoError(t, err)
	defer postgresContainer.Terminate(ctx)

	// Get the host and port of the PostgreSQL container
	host, err := postgresContainer.Host(ctx)
	require.NoError(t, err)
	port, err := postgresContainer.MappedPort(ctx, "5432")
	require.NoError(t, err)
	// host=localhost port=5432 user=postgres password=example dbname=godb sslmode=disable
	// Setup real database connection
	dsn := fmt.Sprintf("host=%s port=%s user=postgres password=example dbname=godb sslmode=disable", host, port.Port())
	db, err := dbconnector.OpenDbConnect(dsn)
	require.NoError(t, err)
	err = db.DBInitialize()
	require.NoError(t, err)

	// Setup loyalty system with real database
	dataChan := make(chan models.Order)
	ls := server.NewServerSystem(db, dataChan, "http://localhost:8080")

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-dataChan:
				//
			}
		}
	}()

	// Setup router and register handler
	r := mux.NewRouter()
	r.HandleFunc("/api/user/load", ls.LoadOrderHandler).Methods("POST")

	// Test cases
	testCases := []struct {
		name           string
		cookie         *http.Cookie
		orderNumber    string
		user           models.User
		existingOrder  models.Order
		expectedStatus int
	}{
		{
			name:           "Valid order",
			cookie:         &http.Cookie{Name: "session_token", Value: "test@example.com"},
			orderNumber:    "3182649",
			user:           models.User{Email: "test@example.com", Password: "password"},
			existingOrder:  models.Order{Number: "3182649"},
			expectedStatus: http.StatusAccepted,
		},
		// Add more test cases as needed
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup database with test data
			err = db.AddUser(&tc.user, ctx)
			require.NoError(t, err)
			//db.Create(&tc.existingOrder)

			// Create request
			body := []byte(tc.orderNumber)
			req, err := http.NewRequest("POST", "/api/user/load", bytes.NewReader(body))
			require.NoError(t, err)
			req.AddCookie(tc.cookie)

			// Create response recorder
			rr := httptest.NewRecorder()

			// Call handler
			r.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, rr.Code)

			// Clean up test data
			db.DeleteUser(&tc.user, ctx)
			//db.Delete(&tc.existingOrder)

			// Add more assertions as needed
		})
	}
}

func TestLunh(t *testing.T) {
	number := "3182649"
	res := server.IsValidLuhn(number)
	assert.Equal(t, true, res)

	number = "11111111"
	res = server.IsValidLuhn(number)
	assert.Equal(t, false, res)
}
