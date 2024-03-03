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
	"github.com/theheadmen/goDipl2/internal/service"
)

type ServerSystem struct {
	Storage service.Storage
	BaseURL string
}

func NewServerSystem(storage service.Storage, baseURL string) *ServerSystem {
	return &ServerSystem{Storage: storage, BaseURL: baseURL}
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
	log.Printf("try to register with email: %s, and password: %s\n", user.Email, user.Password)

	// Сохраняем пользователя в базе данных
	errorCode, err := service.RegisterUserLogic(r.Context(), ls.Storage, user)
	if err != nil {
		http.Error(w, err.Error(), errorCode)
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
	log.Printf("try to login with email: %s, and password: %s\n", reqUser.Email, reqUser.Password)

	respCode, err := service.LoginUserLogic(r.Context(), ls.Storage, reqUser)
	if err != nil {
		http.Error(w, err.Error(), respCode)
		return
	}

	// Устанавливаем cookie для аутентификации
	http.SetCookie(w, &http.Cookie{
		Name:  "session_token",
		Value: reqUser.Email, // В качестве токена используем email пользователя
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
	log.Printf("post order call for %d\n", user.ID)

	// Читаем номер заказа из тела запроса
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	orderNumber := string(body)

	err = service.LoadOrderLogic(r.Context(), orderNumber, ls.Storage, w, user)
	if err != nil {
		// Handle the error
		return
	}

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
	log.Printf("get order call for %d\n", user.ID)

	// Конвертируем список заказов в список ответов
	orderResponses, err := service.GetOrderLogic(r.Context(), ls.Storage, user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Если список заказов пуст, возвращаем 204 No Content
	if len(orderResponses) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
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
	log.Printf("get balance call for %d\n", user.ID)

	balanceResponse, err := service.GetBalanceLogic(r.Context(), ls.Storage, user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Возвращаем ответ в формате JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(balanceResponse)
}

func (ls *ServerSystem) WithdrawHandler(w http.ResponseWriter, r *http.Request) {
	var user dbconnector.User
	err := ls.AuthenticateUser(w, r, &user)
	if err != nil {
		// Handle the error
		return
	}
	log.Printf("post withdraw call for %d\n", user.ID)

	// Декодируем JSON-запрос
	var withdrawRequest models.WithdrawRequest
	err = json.NewDecoder(r.Body).Decode(&withdrawRequest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	log.Printf("Try to minus sum: %f, for order: %s\n", withdrawRequest.Sum, withdrawRequest.Order)

	code, err := service.WithdrawLogic(r.Context(), ls.Storage, user.Email, user.ID, withdrawRequest)

	if err != nil {
		http.Error(w, err.Error(), code)
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
	err = ls.Storage.GetAddWithdrawalsByUserID(r.Context(), user.ID, &withdrawals)
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
	w.WriteHeader(http.StatusOK)
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
	err = ls.Storage.GetUserByEmail(r.Context(), cookie.Value, user)
	if err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return err
	}

	return nil
}
