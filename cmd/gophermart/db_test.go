package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	"github.com/theheadmen/goDipl2/internal/models"
	"github.com/theheadmen/goDipl2/internal/server"
	"github.com/theheadmen/goDipl2/internal/service"
	"golang.org/x/crypto/bcrypt"
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

	suite.ls = server.NewServerSystem(db, "http://localhost:8080")
	suite.router = mux.NewRouter()
	suite.router.HandleFunc("/api/user/register", suite.ls.RegisterUserHandler).Methods("POST")
	suite.router.HandleFunc("/api/user/login", suite.ls.LoginUserHandler).Methods("POST")
	suite.router.HandleFunc("/api/user/orders", suite.ls.LoadOrderHandler).Methods("POST")
	suite.router.HandleFunc("/api/user/orders", suite.ls.GetOrderHandler).Methods("GET")
	suite.router.HandleFunc("/api/user/balance", suite.ls.GetBalanceHandler).Methods("GET")
	suite.router.HandleFunc("/api/user/balance/withdraw", suite.ls.WithdrawHandler).Methods("POST")
	suite.router.HandleFunc("/api/user/withdrawals", suite.ls.GetWithdrawalsHandler).Methods("GET")
}

func (suite *LoyaltySystemTestSuite) TearDownSuite() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(suite.T(), suite.postgres.Terminate(ctx))
}

// LoadOrderHandler
// правильный заказ, http.StatusAccepted
// неправильный номер заказа, http.StatusUnprocessableEntity
// уже был такой заказ, http.StatusOK
// уже был такой заказ, но для другого пользователя, http.StatusConflict
func (suite *LoyaltySystemTestSuite) TestLoyaltySystemLoadOrder() {
	if testing.Short() {
		suite.T().Skip("Skipping integration test")
	}

	// Test cases
	testCases := []struct {
		name                     string
		cookie                   *http.Cookie
		orderNumber              string
		user                     dbconnector.User
		existingOrder            dbconnector.Order
		withExistingOrder        bool
		existingOrderForSameUser bool
		secondUser               dbconnector.User
		expectedStatus           int
	}{
		{
			name:                     "Valid order",
			cookie:                   &http.Cookie{Name: "session_token", Value: "test@example.com"},
			orderNumber:              "3182649",
			user:                     dbconnector.User{Email: "test@example.com", Password: "password"},
			existingOrder:            dbconnector.Order{Number: "3182649"},
			withExistingOrder:        false,
			existingOrderForSameUser: false,
			secondUser:               dbconnector.User{},
			expectedStatus:           http.StatusAccepted,
		},
		{
			name:                     "Invalid order",
			cookie:                   &http.Cookie{Name: "session_token", Value: "test@example.com"},
			orderNumber:              "1",
			user:                     dbconnector.User{Email: "test@example.com", Password: "password"},
			existingOrder:            dbconnector.Order{Number: "3182649"},
			withExistingOrder:        false,
			existingOrderForSameUser: false,
			secondUser:               dbconnector.User{},
			expectedStatus:           http.StatusUnprocessableEntity,
		},
		{
			name:                     "Repeat order",
			cookie:                   &http.Cookie{Name: "session_token", Value: "test@example.com"},
			orderNumber:              "3182649",
			user:                     dbconnector.User{Email: "test@example.com", Password: "password"},
			existingOrder:            dbconnector.Order{Number: "3182649"},
			withExistingOrder:        true,
			existingOrderForSameUser: true,
			secondUser:               dbconnector.User{},
			expectedStatus:           http.StatusOK,
		},
		{
			name:                     "Repeat order for another user",
			cookie:                   &http.Cookie{Name: "session_token", Value: "test@example.com"},
			orderNumber:              "3182649",
			user:                     dbconnector.User{Email: "test@example.com", Password: "password"},
			existingOrder:            dbconnector.Order{Number: "3182649"},
			withExistingOrder:        true,
			existingOrderForSameUser: false,
			secondUser:               dbconnector.User{Email: "test2@example.com", Password: "password"},
			expectedStatus:           http.StatusConflict,
		},
	}

	for _, tc := range testCases {
		suite.T().Run(tc.name, func(t *testing.T) {
			// Clean up test data
			suite.db.DeleteAllData(suite.ctx)
			// Setup database with test data
			err := suite.db.AddUser(suite.ctx, &tc.user)
			require.NoError(t, err)
			if tc.withExistingOrder {
				if tc.existingOrderForSameUser {
					// Нам нужно узнать какой id система дала этому user
					user, err := suite.db.GetUserByEmail(suite.ctx, tc.user.Email)
					require.NoError(t, err)
					tc.existingOrder.UserID = user.ID
					err = suite.db.AddOrder(suite.ctx, &tc.existingOrder)
					require.NoError(t, err)
				} else {
					err := suite.db.AddUser(suite.ctx, &tc.secondUser)
					require.NoError(t, err)
					// Нам нужно узнать какой id система дала этому user
					user, err := suite.db.GetUserByEmail(suite.ctx, tc.secondUser.Email)
					require.NoError(t, err)
					tc.existingOrder.UserID = user.ID
					err = suite.db.AddOrder(suite.ctx, &tc.existingOrder)
					require.NoError(t, err)
				}
			}

			// Create request
			body := []byte(tc.orderNumber)
			req, err := http.NewRequest("POST", "/api/user/orders", bytes.NewReader(body))
			require.NoError(t, err)
			req.AddCookie(tc.cookie)

			// Create response recorder
			rr := httptest.NewRecorder()

			// Call handler
			suite.router.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, rr.Code)

			// Clean up test data
			suite.db.DeleteAllData(suite.ctx)
		})
	}
}

// RegisterUserHandler
// успешная регистрация
// повторная, неуспешная регистрация http.StatusConflict
// неуспешная регистрация без email, http.StatusBadRequest
// неуспешная регистрация без password, http.StatusBadRequest
func (suite *LoyaltySystemTestSuite) TestLoyaltySystemRegister() {
	if testing.Short() {
		suite.T().Skip("Skipping integration test")
	}

	// Test cases
	testCases := []struct {
		name           string
		firstUser      dbconnector.User
		secondUser     dbconnector.User
		useSecondUser  bool
		expectedStatus int
	}{
		{
			name:           "Valid register",
			firstUser:      dbconnector.User{Email: "test@example.com", Password: "password"},
			secondUser:     dbconnector.User{Email: "test@example.com", Password: "password"},
			useSecondUser:  false,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid register, no email",
			firstUser:      dbconnector.User{Email: "", Password: "password"},
			secondUser:     dbconnector.User{Email: "test@example.com", Password: "password"},
			useSecondUser:  false,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid register, no pass",
			firstUser:      dbconnector.User{Email: "test@example.com", Password: ""},
			secondUser:     dbconnector.User{Email: "test@example.com", Password: "password"},
			useSecondUser:  false,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid register, repeat",
			firstUser:      dbconnector.User{Email: "test@example.com", Password: "password"},
			secondUser:     dbconnector.User{Email: "test@example.com", Password: "password"},
			useSecondUser:  true,
			expectedStatus: http.StatusConflict,
		},
	}

	for _, tc := range testCases {
		suite.T().Run(tc.name, func(t *testing.T) {
			// Перестраховка на всякий случай
			suite.db.DeleteAllData(suite.ctx)
			// Setup database with test data
			if tc.useSecondUser {
				err := suite.db.AddUser(suite.ctx, &tc.secondUser)
				require.NoError(t, err)
			}

			// Create request
			body, err := json.Marshal(tc.firstUser)
			require.NoError(t, err)
			req, err := http.NewRequest("POST", "/api/user/register", bytes.NewReader(body))
			require.NoError(t, err)

			// Create response recorder
			rr := httptest.NewRecorder()

			// Call handler
			suite.router.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, rr.Code)
			if tc.expectedStatus == http.StatusOK {
				resp := rr.Result()
				defer resp.Body.Close()
				cookies := resp.Cookies()
				for _, cookie := range cookies {
					assert.Equal(t, cookie.Name, "session_token")
					assert.Equal(t, cookie.Value, tc.firstUser.Email)
				}
			}

			// Clean up test data
			suite.db.DeleteAllData(suite.ctx)
		})
	}
}

// LoginUserHandler
// успешный логин
// неуспешный логин, нет email, http.StatusBadRequest
// неуспешный логин, нет Password, http.StatusBadRequest
// неуспешный логин, неправильный password, http.StatusUnauthorized
func (suite *LoyaltySystemTestSuite) TestLoyaltySystemLogin() {
	if testing.Short() {
		suite.T().Skip("Skipping integration test")
	}

	// Test cases
	testCases := []struct {
		name           string
		correctUser    dbconnector.User
		testUser       dbconnector.User
		expectedStatus int
	}{
		{
			name:           "Valid login",
			correctUser:    dbconnector.User{Email: "test@example.com", Password: "password"},
			testUser:       dbconnector.User{Email: "test@example.com", Password: "password"},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid login, no email",
			correctUser:    dbconnector.User{Email: "test@example.com", Password: "password"},
			testUser:       dbconnector.User{Email: "", Password: "password"},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid login, no pass",
			correctUser:    dbconnector.User{Email: "test@example.com", Password: "password"},
			testUser:       dbconnector.User{Email: "test@example.com", Password: ""},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid login, wrong pass",
			correctUser:    dbconnector.User{Email: "test@example.com", Password: "password"},
			testUser:       dbconnector.User{Email: "test@example.com", Password: "password23"},
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range testCases {
		suite.T().Run(tc.name, func(t *testing.T) {
			// Перестраховка на всякий случай
			suite.db.DeleteAllData(suite.ctx)
			// Setup database with test data
			// так как дальше мы будем проверять схешированные данные, нужно зашифровать пароль
			hashedPassword, err := bcrypt.GenerateFromPassword([]byte(tc.correctUser.Password), bcrypt.DefaultCost)
			require.NoError(t, err)
			tc.correctUser.Password = string(hashedPassword)
			err = suite.db.AddUser(suite.ctx, &tc.correctUser)
			require.NoError(t, err)

			// Create request
			body, err := json.Marshal(tc.testUser)
			require.NoError(t, err)
			req, err := http.NewRequest("POST", "/api/user/login", bytes.NewReader(body))
			require.NoError(t, err)

			// Create response recorder
			rr := httptest.NewRecorder()

			// Call handler
			suite.router.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, rr.Code)
			if tc.expectedStatus == http.StatusOK {
				resp := rr.Result()
				defer resp.Body.Close()
				cookies := resp.Cookies()
				for _, cookie := range cookies {
					assert.Equal(t, cookie.Name, "session_token")
					assert.Equal(t, cookie.Value, tc.testUser.Email)
				}
			}

			// Clean up test data
			suite.db.DeleteAllData(suite.ctx)
		})
	}
}

// GetOrderHandler
// нет заказов, http.StatusNoContent
// есть, один заказ, http.StatusOK
// есть, несколько заказов, http.StatusOK
func (suite *LoyaltySystemTestSuite) TestLoyaltySystemGetOrder() {
	if testing.Short() {
		suite.T().Skip("Skipping integration test")
	}

	// Test cases
	testCases := []struct {
		name           string
		cookie         *http.Cookie
		user           dbconnector.User
		orders         []dbconnector.Order
		expectedStatus int
	}{
		{
			name:           "Valid orders #1",
			cookie:         &http.Cookie{Name: "session_token", Value: "test@example.com"},
			user:           dbconnector.User{Email: "test@example.com", Password: "password"},
			orders:         []dbconnector.Order{{Number: "1"}},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Valid orders #2",
			cookie:         &http.Cookie{Name: "session_token", Value: "test@example.com"},
			user:           dbconnector.User{Email: "test@example.com", Password: "password"},
			orders:         []dbconnector.Order{{Number: "1"}, {Number: "2"}},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Empty orders",
			cookie:         &http.Cookie{Name: "session_token", Value: "test@example.com"},
			user:           dbconnector.User{Email: "test@example.com", Password: "password"},
			orders:         []dbconnector.Order{},
			expectedStatus: http.StatusNoContent,
		},
	}

	for _, tc := range testCases {
		suite.T().Run(tc.name, func(t *testing.T) {
			// Перестраховка на всякий случай
			suite.db.DeleteAllData(suite.ctx)
			// Setup database with test data
			err := suite.db.AddUser(suite.ctx, &tc.user)
			require.NoError(t, err)
			// Нам нужно узнать какой id система дала этому user
			user, err := suite.db.GetUserByEmail(suite.ctx, tc.user.Email)
			require.NoError(t, err)
			for _, w := range tc.orders {
				// назначаем реальный userID
				w.UserID = user.ID
				err := suite.db.AddOrder(suite.ctx, &w)
				require.NoError(t, err)
			}

			// Create request
			body := []byte{}
			req, err := http.NewRequest("GET", "/api/user/orders", bytes.NewReader(body))
			require.NoError(t, err)
			req.AddCookie(tc.cookie)

			// Create response recorder
			rr := httptest.NewRecorder()

			// Call handler
			suite.router.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, rr.Code)

			if len(tc.orders) != 0 {
				var orderResponses []models.OrderResponse
				err = json.NewDecoder(rr.Body).Decode(&orderResponses)
				require.NoError(t, err)
				assert.Equal(t, len(tc.orders), len(orderResponses))
				for i, w := range orderResponses {
					assert.Equal(t, 0.0, w.Accrual)
					assert.Equal(t, "NEW", w.Status)
					assert.Equal(t, tc.orders[i].Number, w.Number)
				}
			}

			// Clean up test data
			suite.db.DeleteAllData(suite.ctx)
		})
	}
}

// GetBalanceHandler
// корректная сумма, нет списаний, http.StatusOK
// корректная сумма, есть списания, http.StatusOK
func (suite *LoyaltySystemTestSuite) TestLoyaltySystemGetBalance() {
	if testing.Short() {
		suite.T().Skip("Skipping integration test")
	}

	// Test cases
	testCases := []struct {
		name            string
		cookie          *http.Cookie
		user            dbconnector.User
		withdrawals     []dbconnector.Withdrawal
		balanceResponse models.BalanceResponse
		expectedStatus  int
	}{
		{
			name:            "Valid balance with withdrawal #1",
			cookie:          &http.Cookie{Name: "session_token", Value: "test@example.com"},
			user:            dbconnector.User{Email: "test@example.com", Password: "password", Balance: 100.0},
			withdrawals:     []dbconnector.Withdrawal{{Points: 100.0, Number: "1"}},
			balanceResponse: models.BalanceResponse{Current: 100.0, Withdrawn: 100.0},
			expectedStatus:  http.StatusOK,
		},
		{
			name:            "Valid balance with withdrawal #2",
			cookie:          &http.Cookie{Name: "session_token", Value: "test@example.com"},
			user:            dbconnector.User{Email: "test@example.com", Password: "password", Balance: 50.0},
			withdrawals:     []dbconnector.Withdrawal{{Points: 100.0, Number: "1"}, {Points: 150.0, Number: "2"}},
			balanceResponse: models.BalanceResponse{Current: 50.0, Withdrawn: 250.0},
			expectedStatus:  http.StatusOK,
		},
		{
			name:            "Valid balance without withdrawal",
			cookie:          &http.Cookie{Name: "session_token", Value: "test@example.com"},
			user:            dbconnector.User{Email: "test@example.com", Password: "password", Balance: 100.0},
			withdrawals:     []dbconnector.Withdrawal{},
			balanceResponse: models.BalanceResponse{Current: 100.0, Withdrawn: 0.0},
			expectedStatus:  http.StatusOK,
		},
	}

	for _, tc := range testCases {
		suite.T().Run(tc.name, func(t *testing.T) {
			// Перестраховка на всякий случай
			suite.db.DeleteAllData(suite.ctx)
			// Setup database with test data
			err := suite.db.AddUser(suite.ctx, &tc.user)
			require.NoError(t, err)
			// Нам нужно узнать какой id система дала этому user
			user, err := suite.db.GetUserByEmail(suite.ctx, tc.user.Email)
			require.NoError(t, err)
			for _, w := range tc.withdrawals {
				// назначаем реальный userID
				w.UserID = user.ID
				err := suite.db.AddWithdrawal(suite.ctx, &w)
				require.NoError(t, err)
			}

			// Create request
			body := []byte{}
			req, err := http.NewRequest("GET", "/api/user/balance", bytes.NewReader(body))
			require.NoError(t, err)
			req.AddCookie(tc.cookie)

			// Create response recorder
			rr := httptest.NewRecorder()

			// Call handler
			suite.router.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, rr.Code)

			var balanceResponse models.BalanceResponse
			err = json.NewDecoder(rr.Body).Decode(&balanceResponse)
			require.NoError(t, err)
			assert.Equal(t, tc.balanceResponse, balanceResponse)

			// Clean up test data
			suite.db.DeleteAllData(suite.ctx)
		})
	}
}

// WithdrawHandler
// хватает денег, списание, http.StatusOK
// не хватает денег, http.StatusPaymentRequired
func (suite *LoyaltySystemTestSuite) TestLoyaltySystemWithdrawal() {
	if testing.Short() {
		suite.T().Skip("Skipping integration test")
	}

	// Test cases
	testCases := []struct {
		name           string
		cookie         *http.Cookie
		user           dbconnector.User
		withdrawal     models.WithdrawRequest
		expectedStatus int
	}{
		{
			name:           "Valid withdrawal",
			cookie:         &http.Cookie{Name: "session_token", Value: "test@example.com"},
			user:           dbconnector.User{Email: "test@example.com", Password: "password", Balance: 500},
			withdrawal:     models.WithdrawRequest{Sum: 100.0, Order: "1"},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid withdrawal",
			cookie:         &http.Cookie{Name: "session_token", Value: "test@example.com"},
			user:           dbconnector.User{Email: "test@example.com", Password: "password", Balance: 500},
			withdrawal:     models.WithdrawRequest{Sum: 1000.0, Order: "1"},
			expectedStatus: http.StatusPaymentRequired,
		},
	}

	for _, tc := range testCases {
		suite.T().Run(tc.name, func(t *testing.T) {
			// Перестраховка на всякий случай
			suite.db.DeleteAllData(suite.ctx)
			// Setup database with test data
			err := suite.db.AddUser(suite.ctx, &tc.user)
			require.NoError(t, err)

			// Create request
			body, err := json.Marshal(tc.withdrawal)
			require.NoError(t, err)
			req, err := http.NewRequest("POST", "/api/user/balance/withdraw", bytes.NewReader(body))
			require.NoError(t, err)
			req.AddCookie(tc.cookie)

			// Create response recorder
			rr := httptest.NewRecorder()

			// Call handler
			suite.router.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, rr.Code)

			// Clean up test data
			suite.db.DeleteAllData(suite.ctx)
		})
	}
}

// GetWithdrawalsHandler
// нет списаний, http.StatusNoContent
// есть списания, http.StatusOK
func (suite *LoyaltySystemTestSuite) TestLoyaltySystemGetWithdrawal() {
	if testing.Short() {
		suite.T().Skip("Skipping integration test")
	}

	// Test cases
	testCases := []struct {
		name           string
		cookie         *http.Cookie
		user           dbconnector.User
		withdrawals    []dbconnector.Withdrawal
		expectedStatus int
	}{
		{
			name:           "Valid withdrawal #1",
			cookie:         &http.Cookie{Name: "session_token", Value: "test@example.com"},
			user:           dbconnector.User{Email: "test@example.com", Password: "password"},
			withdrawals:    []dbconnector.Withdrawal{{Points: 100.0, UserID: 1, Number: "1"}},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Valid withdrawal #2",
			cookie:         &http.Cookie{Name: "session_token", Value: "test@example.com"},
			user:           dbconnector.User{Email: "test@example.com", Password: "password"},
			withdrawals:    []dbconnector.Withdrawal{{Points: 100.0, Number: "1"}, {Points: 200.0, Number: "2"}},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Empty withdrawal",
			cookie:         &http.Cookie{Name: "session_token", Value: "test@example.com"},
			user:           dbconnector.User{Email: "test@example.com", Password: "password"},
			withdrawals:    []dbconnector.Withdrawal{},
			expectedStatus: http.StatusNoContent,
		},
	}

	for _, tc := range testCases {
		suite.T().Run(tc.name, func(t *testing.T) {
			// Перестраховка на всякий случай
			suite.db.DeleteAllData(suite.ctx)
			// Setup database with test data
			err := suite.db.AddUser(suite.ctx, &tc.user)
			require.NoError(t, err)
			// Нам нужно узнать какой id система дала этому user
			user, err := suite.db.GetUserByEmail(suite.ctx, tc.user.Email)
			require.NoError(t, err)
			for _, w := range tc.withdrawals {
				// назначаем реальный userID
				w.UserID = user.ID
				err := suite.db.AddWithdrawal(suite.ctx, &w)
				require.NoError(t, err)
			}

			// Create request
			body := []byte{}
			req, err := http.NewRequest("GET", "/api/user/withdrawals", bytes.NewReader(body))
			require.NoError(t, err)
			req.AddCookie(tc.cookie)

			// Create response recorder
			rr := httptest.NewRecorder()

			// Call handler
			suite.router.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, rr.Code)

			if len(tc.withdrawals) != 0 {
				var withdrawRequests []models.WithdrawalResponse
				err = json.NewDecoder(rr.Body).Decode(&withdrawRequests)
				require.NoError(t, err)
				assert.Equal(t, len(tc.withdrawals), len(withdrawRequests))
				for i, w := range withdrawRequests {
					assert.Equal(t, tc.withdrawals[i].Points, w.Sum)
					assert.Equal(t, tc.withdrawals[i].Number, w.Order)
				}
			}

			// Clean up test data
			suite.db.DeleteAllData(suite.ctx)
		})
	}
}

func (suite *LoyaltySystemTestSuite) TestLunh() {
	number := "3182649"
	res := service.IsValidLuhn(number)
	assert.Equal(suite.T(), true, res)

	number = "11111111"
	res = service.IsValidLuhn(number)
	assert.Equal(suite.T(), false, res)
}

func TestLoyaltySystemTestSuite(t *testing.T) {
	suite.Run(t, new(LoyaltySystemTestSuite))
}
