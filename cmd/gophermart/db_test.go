package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/theheadmen/goDipl2/internal/dbconnector"
	"github.com/theheadmen/goDipl2/internal/server"
)

type Config struct {
	Host     string
	Port     uint16
	Username string
	Password string
	DBName   string
}

type LoyaltySystemTestSuite struct {
	suite.Suite
	db       *dbconnector.DBConnector
	ls       *server.ServerSystem
	router   *mux.Router
	postgres testcontainers.Container
	ctx      context.Context
	wg       *sync.WaitGroup
	dataChan chan dbconnector.Order
}

func (suite *LoyaltySystemTestSuite) SetupSuite() {
	cfg := &Config{
		Username: "postgres",
		Password: "example",
		DBName:   "godb",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	suite.ctx = ctx

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

	require.NoError(suite.T(), err)
	suite.postgres = postgresContainer

	host, err := postgresContainer.Host(ctx)
	require.NoError(suite.T(), err)
	port, err := postgresContainer.MappedPort(ctx, "5432")
	require.NoError(suite.T(), err)
	dsn := fmt.Sprintf("host=%s port=%s user=postgres password=example dbname=godb sslmode=disable", host, port.Port())
	db, err := dbconnector.OpenDBConnect(dsn)
	require.NoError(suite.T(), err)
	err = db.DBInitialize()
	require.NoError(suite.T(), err)

	suite.db = db
	// Канал для получения данных
	dataChan := make(chan dbconnector.Order)
	var wg sync.WaitGroup
	suite.dataChan = dataChan
	suite.wg = &wg

	suite.ls = server.NewServerSystem(db, "http://localhost:8080", dataChan)
	suite.router = mux.NewRouter()
	suite.router.HandleFunc("/api/user/load", suite.ls.LoadOrderHandler).Methods("POST")
}

func (suite *LoyaltySystemTestSuite) dataChanDummy() {
	suite.wg.Add(1)
	go func() {
		defer suite.wg.Done()
		for {
			select {
			case <-suite.ctx.Done():
				return
			case data, opened := <-suite.dataChan:
				if !opened {
					return
				}
				suite.T().Log(data)
			}
		}
	}()
}

func (suite *LoyaltySystemTestSuite) TearDownSuite() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	close(suite.dataChan) //
	suite.wg.Wait()

	require.NoError(suite.T(), suite.postgres.Terminate(ctx))
}

func (suite *LoyaltySystemTestSuite) TestLoyaltySystemWithTestContainer() {
	if testing.Short() {
		suite.T().Skip("Skipping integration test")
	}

	// Test cases
	testCases := []struct {
		name           string
		cookie         *http.Cookie
		orderNumber    string
		user           dbconnector.User
		existingOrder  dbconnector.Order
		expectedStatus int
	}{
		{
			name:           "Valid order",
			cookie:         &http.Cookie{Name: "session_token", Value: "test@example.com"},
			orderNumber:    "3182649",
			user:           dbconnector.User{Email: "test@example.com", Password: "password"},
			existingOrder:  dbconnector.Order{Number: "3182649"},
			expectedStatus: http.StatusAccepted,
		},
		// Add more test cases as needed
	}

	for _, tc := range testCases {
		suite.T().Run(tc.name, func(t *testing.T) {
			suite.dataChanDummy()
			// Setup database with test data
			err := suite.db.AddUser(suite.ctx, &tc.user)
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
			suite.router.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, rr.Code)

			// Clean up test data
			suite.db.DeleteUser(suite.ctx, &tc.user)
			//db.Delete(&tc.existingOrder)

			// Add more assertions as needed
		})
	}
}

func (suite *LoyaltySystemTestSuite) TestLunh() {
	number := "3182649"
	res := server.IsValidLuhn(number)
	assert.Equal(suite.T(), true, res)

	number = "11111111"
	res = server.IsValidLuhn(number)
	assert.Equal(suite.T(), false, res)
}

func TestLoyaltySystemTestSuite(t *testing.T) {
	suite.Run(t, new(LoyaltySystemTestSuite))
}
