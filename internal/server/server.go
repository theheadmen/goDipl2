package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/theheadmen/goDipl2/internal/dbconnector"
	"github.com/theheadmen/goDipl2/internal/models"
	"golang.org/x/crypto/bcrypt"
)

type ServerSystem struct {
	DB       *dbconnector.DBConnector
	Datachan chan models.Order
	BaseURL  string
}

func NewServerSystem(db *dbconnector.DBConnector, datachan chan models.Order, baseURL string) *ServerSystem {
	return &ServerSystem{DB: db, Datachan: datachan, BaseURL: baseURL}
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
	var user models.User
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
	err = ls.DB.AddUser(&user, r.Context())
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
	var reqUser models.User
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
	var user models.User
	err = ls.DB.GetUserByEmail(reqUser.Email, &user, r.Context())
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

func IsValidLuhn(number string) bool {
	digits := len(number)
	parity := digits % 2
	sum := 0
	for i := 0; i < digits; i++ {
		digit, err := strconv.Atoi(string(number[i]))
		if err != nil {
			return false // Если символ не является цифрой, возвращаем false
		}
		if i%2 == parity {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	return sum%10 == 0
}

func (ls *ServerSystem) LoadOrderHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяем аутентификацию пользователя
	cookie, err := r.Cookie("session_token")
	if err != nil {
		http.Error(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Ищем пользователя в базе данных
	var user models.User
	err = ls.DB.GetUserByEmail(cookie.Value, &user, r.Context())
	if err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}
	log.Printf("load orders call for %d\n", user.ID)

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
	var existingOrder models.Order
	err = ls.DB.GetOrderByNumber(orderNumber, &existingOrder, r.Context())
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
	order := models.Order{
		Number: orderNumber,
		UserID: user.ID,
	}

	// Сохраняем заказ в базе данных
	err = ls.DB.AddOrder(&order, r.Context())
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
	// Проверяем аутентификацию пользователя
	cookie, err := r.Cookie("session_token")
	if err != nil {
		http.Error(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Ищем пользователя в базе данных
	var user models.User
	err = ls.DB.GetUserByEmail(cookie.Value, &user, r.Context())
	if err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}
	log.Printf("get orders call for %d\n", user.ID)

	// Получаем список заказов пользователя
	var orders []models.Order
	err = ls.DB.GetOrdersByUserID(user.ID, &orders, r.Context())
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
	json.NewEncoder(w).Encode(orderResponses)
}

func (ls *ServerSystem) GetBalanceHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяем аутентификацию пользователя
	cookie, err := r.Cookie("session_token")
	if err != nil {
		http.Error(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Ищем пользователя в базе данных
	var user models.User
	err = ls.DB.GetUserByEmail(cookie.Value, &user, r.Context())
	if err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}
	log.Printf("get balance call for %d\n", user.ID)

	// Получаем сумму использованных баллов
	var withdrawn float64
	err = ls.DB.GetSumOfWithdrawalByUserID(user.ID, &withdrawn, r.Context())

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
	// Проверяем аутентификацию пользователя
	cookie, err := r.Cookie("session_token")
	if err != nil {
		http.Error(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Ищем пользователя в базе данных
	var user models.User
	err = ls.DB.GetUserByEmail(cookie.Value, &user, r.Context())
	if err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}
	log.Printf("withdraw call for %d\n", user.ID)

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
	order := models.Order{
		Number: withdrawRequest.Order,
		UserID: user.ID,
		Status: "PROCESSED", // Предполагаем, что списание сразу обрабатывается
	}

	// Сохраняем заказ в базе данных
	err = ls.DB.AddOrder(&order, r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Создаем списание
	withdrawal := models.Withdrawal{
		Points: withdrawRequest.Sum,
		UserID: user.ID,
		Number: withdrawRequest.Order,
	}
	// Сохраняем списание в базе данных
	err = ls.DB.AddWithdrawal(&withdrawal, r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Обновляем баланс пользователя
	user.Balance -= withdrawRequest.Sum
	err = ls.DB.UpdateUser(&user, r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (ls *ServerSystem) GetWithdrawalsHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяем аутентификацию пользователя
	cookie, err := r.Cookie("session_token")
	if err != nil {
		http.Error(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Ищем пользователя в базе данных
	var user models.User
	err = ls.DB.GetUserByEmail(cookie.Value, &user, r.Context())
	if err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}
	log.Printf("get withdraw call for %d\n", user.ID)

	// Получаем список выводов средств пользователя
	var withdrawals []models.Withdrawal
	err = ls.DB.GetAddWithdrawalsByUserID(user.ID, &withdrawals, r.Context())
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
