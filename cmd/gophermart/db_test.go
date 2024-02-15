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
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	// Setup loyalty system with real database
	dataChan := make(chan Order)
	ls := NewLoyaltySystem(db, dataChan, "http://localhost:8080")
	err = ls.Initialize()
	require.NoError(t, err)

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
		user           User
		existingOrder  Order
		expectedStatus int
	}{
		{
			name:           "Valid order",
			cookie:         &http.Cookie{Name: "session_token", Value: "test@example.com"},
			orderNumber:    "3182649",
			user:           User{Email: "test@example.com", Password: "password"},
			existingOrder:  Order{Number: "3182649"},
			expectedStatus: http.StatusAccepted,
		},
		// Add more test cases as needed
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup database with test data
			db.Create(&tc.user)
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
			db.Delete(&tc.user)
			//db.Delete(&tc.existingOrder)

			// Add more assertions as needed
		})
	}
}

func TestLunh(t *testing.T) {
	number := "3182649"
	res := IsValidLuhn(number)
	assert.Equal(t, true, res)

	number = "11111111"
	res = IsValidLuhn(number)
	assert.Equal(t, false, res)
}
