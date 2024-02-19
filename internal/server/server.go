package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/theheadmen/goDipl2/internal/dbconnector"
	"github.com/theheadmen/goDipl2/internal/models"
	"golang.org/x/crypto/bcrypt"
)

type ServerSystem struct {
	DB       *dbconnector.DBConnector
	Datachan chan dbconnector.Order
	BaseURL  string
}

func NewServerSystem(db *dbconnector.DBConnector, baseURL string, dataChan chan dbconnector.Order) *ServerSystem {
	return &ServerSystem{DB: db, Datachan: dataChan, BaseURL: baseURL}
}

func (ls *ServerSystem) MakeServer(serverAddr string) *http.Server {
	r := mux.NewRouter()
	r.HandleFunc("/api/user/register", ls.RegisterUserHandler).Methods("POST")
	r.HandleFunc("/api/user/login", ls.LoginUserHandler).Methods("POST")
	r.HandleFunc("/api/user/orders", ls.LoadOrderHandler).Methods("POST")
	r.HandleFunc("/api/user/orders", ls.GetOrderHandler).Methods("GET")
	r.HandleFunc("/api/user/balance", ls.GetBalanceHandler).Methods("GET")
	r.HandleFunc("/api/user/balance/withdraw", ls.WithdrawHandler).Methods("POST")
	r.HandleFunc("/api/user/withdrawals", ls.GetWithdrawalsHandler).Methods("GET")

	server := http.Server{
		Addr:    serverAddr,
		Handler: r,
		// Good practice: enforce timeouts for servers you create!
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	return &server
}

func (ls *ServerSystem) RegisterUserHandler(w http.ResponseWriter, r *http.Request) {
	var user dbconnector.User
	err := json.NewDecoder(r.Body).Decode(&user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Проверяем, что логин и пароль не пустые
	if user.Email == "" || user.Password == "" {
		http.Error(w, "Login and password are required", http.StatusBadRequest)
		return
	}

	// Хешируем пароль
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	user.Password = string(hashedPassword)

	// Сохраняем пользователя в базе данных
	err = ls.DB.AddUser(r.Context(), &user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	// Устанавливаем cookie для аутентификации
	http.SetCookie(w, &http.Cookie{
		Name:  "session_token",
		Value: user.Email, // В качестве токена используем email пользователя
		Path:  "/",
	})

	w.WriteHeader(http.StatusOK)
}

func (ls *ServerSystem) LoginUserHandler(w http.ResponseWriter, r *http.Request) {
	var reqUser dbconnector.User
	err := json.NewDecoder(r.Body).Decode(&reqUser)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Проверяем, что логин и пароль не пустые
	if reqUser.Email == "" || reqUser.Password == "" {
		http.Error(w, "Login and password are required", http.StatusBadRequest)
		return
	}

	// Ищем пользователя в базе данных
	var user dbconnector.User
	err = ls.DB.GetUserByEmail(r.Context(), reqUser.Email, &user)
	if err != nil {
		http.Error(w, "Invalid login or password", http.StatusUnauthorized)
		return
	}

	// Проверяем пароль
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(reqUser.Password))
	if err != nil {
		http.Error(w, "Invalid login or password", http.StatusUnauthorized)
		return
	}

	// Устанавливаем cookie для аутентификации
	http.SetCookie(w, &http.Cookie{
		Name:  "session_token",
		Value: user.Email, // В качестве токена используем email пользователя
		Path:  "/",
	})

	w.WriteHeader(http.StatusOK)
}

func (ls *ServerSystem) LoadOrderHandler(w http.ResponseWriter, r *http.Request) {
	var user dbconnector.User
	err := ls.AuthenticateUser(w, r, &user)
	if err != nil {
		// Handle the error
		return
	}
	log.Printf("get withdraw call for %d\n", user.ID)

	// Читаем номер заказа из тела запроса
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	orderNumber := string(body)

	// Проверяем корректность номера заказа
	if !IsValidLuhn(orderNumber) {
		log.Printf("For user %d, get incorrect order: %s\n", user.ID, orderNumber)
		http.Error(w, "Invalid order number format", http.StatusUnprocessableEntity)
		return
	}

	log.Printf("For user %d, get new order: %s\n", user.ID, orderNumber)

	// Проверяем, не был ли загружен этот заказ другим пользователем
	var existingOrder dbconnector.Order
	err = ls.DB.GetOrderByNumber(r.Context(), orderNumber, &existingOrder)
	if err == nil {
		if existingOrder.UserID == user.ID {
			log.Printf("For user %d, we already have order: %s\n", user.ID, orderNumber)
			w.WriteHeader(http.StatusOK)
			return
		} else {
			log.Printf("For user %d, we can't add order: %s because we already have it for other user %d\n", user.ID, orderNumber, existingOrder.UserID)
			w.WriteHeader(http.StatusConflict)
			return
		}
	}

	// Создаем новый заказ
	order := dbconnector.Order{
		Number: orderNumber,
		UserID: user.ID,
	}

	// Сохраняем заказ в базе данных
	err = ls.DB.AddOrder(r.Context(), &order)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// отправляем заказ в канал на обработку
	ls.Datachan <- order

	log.Printf("For user %d, saved new order: %s\n", user.ID, orderNumber)

	w.WriteHeader(http.StatusAccepted)
}

func (ls *ServerSystem) GetOrderHandler(w http.ResponseWriter, r *http.Request) {
	var user dbconnector.User
	err := ls.AuthenticateUser(w, r, &user)
	if err != nil {
		// Handle the error
		return
	}
	log.Printf("get withdraw call for %d\n", user.ID)

	// Получаем список заказов пользователя
	var orders []dbconnector.Order
	err = ls.DB.GetOrdersByUserID(r.Context(), user.ID, &orders)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Если список заказов пуст, возвращаем 204 No Content
	if len(orders) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Конвертируем список заказов в список ответов
	orderResponses := make([]models.OrderResponse, len(orders))
	for i, order := range orders {
		orderResponses[i] = models.OrderResponse{
			Number:     order.Number,
			Status:     order.Status,
			UploadedAt: order.CreatedAt,
			Accrual:    order.Points,
		}
	}

	// Возвращаем список заказов в формате JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(orderResponses)
}

func (ls *ServerSystem) GetBalanceHandler(w http.ResponseWriter, r *http.Request) {
	var user dbconnector.User
	err := ls.AuthenticateUser(w, r, &user)
	if err != nil {
		// Handle the error
		return
	}
	log.Printf("get withdraw call for %d\n", user.ID)

	// Получаем сумму использованных баллов
	withdrawn := 0.0
	var withdrawals []dbconnector.Withdrawal
	err = ls.DB.GetAddWithdrawalsByUserID(r.Context(), user.ID, &withdrawals)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, withdrawal := range withdrawals {
		log.Printf("we have withdrawal with number %s, points %f\n", withdrawal.Number, withdrawal.Points)
		withdrawn += withdrawal.Points
	}

	log.Printf("get balance for user %d, current %f, withdrawn %f\n", user.ID, user.Balance, withdrawn)

	// Формируем ответ
	balanceResponse := models.BalanceResponse{
		Current:   user.Balance,
		Withdrawn: withdrawn,
	}

	// Возвращаем ответ в формате JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(balanceResponse)
}

func (ls *ServerSystem) WithdrawHandler(w http.ResponseWriter, r *http.Request) {
	var user dbconnector.User
	err := ls.AuthenticateUser(w, r, &user)
	if err != nil {
		// Handle the error
		return
	}
	log.Printf("get withdraw call for %d\n", user.ID)

	// Декодируем JSON-запрос
	var withdrawRequest models.WithdrawRequest
	err = json.NewDecoder(r.Body).Decode(&withdrawRequest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	log.Printf("Try to minus sum: %f, for order: %s\n", withdrawRequest.Sum, withdrawRequest.Order)

	// Проверяем, достаточно ли средств на счете пользователя
	if user.Balance < withdrawRequest.Sum {
		log.Println("but user don't have enough money")
		http.Error(w, "Insufficient funds", http.StatusPaymentRequired)
		return
	}

	// Создаем новый заказ на списание
	order := dbconnector.Order{
		Number: withdrawRequest.Order,
		UserID: user.ID,
		Status: "PROCESSED", // Предполагаем, что списание сразу обрабатывается
	}
	// Создаем списание
	withdrawal := dbconnector.Withdrawal{
		Points: withdrawRequest.Sum,
		UserID: user.ID,
		Number: withdrawRequest.Order,
	}
	// Обновляем баланс пользователя
	user.Balance -= withdrawRequest.Sum
	// отправляем Order, withdrawal и обновляем user - но врамках одной транзакции
	err = ls.DB.WithdrawalTransaction(r.Context(), &order, &withdrawal, &user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (ls *ServerSystem) GetWithdrawalsHandler(w http.ResponseWriter, r *http.Request) {
	var user dbconnector.User
	err := ls.AuthenticateUser(w, r, &user)
	if err != nil {
		// Handle the error
		return
	}
	log.Printf("get withdraw call for %d\n", user.ID)

	// Получаем список выводов средств пользователя
	var withdrawals []dbconnector.Withdrawal
	err = ls.DB.GetAddWithdrawalsByUserID(r.Context(), user.ID, &withdrawals)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Если список выводов пуст, возвращаем 204 No Content
	if len(withdrawals) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Конвертируем список выводов в список ответов
	withdrawalResponses := make([]models.WithdrawalResponse, len(withdrawals))
	for i, withdrawal := range withdrawals {
		log.Printf("get withdrawal with number %s, points %f\n", withdrawal.Number, withdrawal.Points)
		withdrawalResponses[i] = models.WithdrawalResponse{
			Order:       withdrawal.Number,
			Sum:         withdrawal.Points,
			ProcessedAt: withdrawal.CreatedAt,
		}
	}

	// Возвращаем список выводов в формате JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(withdrawalResponses)
}

// AuthenticateUser authenticates the user and looks up the user in the database.
func (ls *ServerSystem) AuthenticateUser(w http.ResponseWriter, r *http.Request, user *dbconnector.User) error {
	// Проверяем аутентификацию пользователя
	cookie, err := r.Cookie("session_token")
	if err != nil {
		http.Error(w, "User not authenticated", http.StatusUnauthorized)
		return err
	}

	// Ищем пользователя в базе данных
	err = ls.DB.GetUserByEmail(r.Context(), cookie.Value, user)
	if err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return err
	}

	return nil
}
