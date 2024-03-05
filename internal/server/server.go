package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/theheadmen/goDipl2/internal/dbconnector"
	"github.com/theheadmen/goDipl2/internal/errors"
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

	logicSystem := service.LogicSystem{Ctx: r.Context(), Storage: ls.Storage, User: &user}
	// Сохраняем пользователя в базе данных
	errorCode, err := logicSystem.RegisterUserLogic()
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

	logicSystem := service.LogicSystem{Ctx: r.Context(), Storage: ls.Storage, User: &reqUser}
	respCode, err := logicSystem.LoginUserLogic()
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
	user, err := ls.AuthenticateUser(w, r)
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

	logicSystem := service.LogicSystem{Ctx: r.Context(), Storage: ls.Storage, User: user}
	err = logicSystem.LoadOrderLogic(orderNumber)
	if err != nil {
		if err == errors.ErrInvalidOrderNumber {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		// заказ есть, для этого пользователя
		if err == errors.ErrAlreadyHaveOrder {
			w.WriteHeader(http.StatusOK)
			return
		}
		// заказ есть, для другого пользователя
		if err == errors.ErrAlreadyHaveOrderForOtherUser {
			w.WriteHeader(http.StatusConflict)
			return
		}
		// любая другая ошибка
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("For user %d, saved new order: %s\n", user.ID, orderNumber)

	w.WriteHeader(http.StatusAccepted)
}

func (ls *ServerSystem) GetOrderHandler(w http.ResponseWriter, r *http.Request) {
	user, err := ls.AuthenticateUser(w, r)
	if err != nil {
		// Handle the error
		return
	}
	log.Printf("get order call for %d\n", user.ID)

	logicSystem := service.LogicSystem{Ctx: r.Context(), Storage: ls.Storage, User: user}
	// Конвертируем список заказов в список ответов
	orderResponses, err := logicSystem.GetOrderLogic()
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
	user, err := ls.AuthenticateUser(w, r)
	if err != nil {
		// Handle the error
		return
	}
	log.Printf("get balance call for %d\n", user.ID)
	logicSystem := service.LogicSystem{Ctx: r.Context(), Storage: ls.Storage, User: user}
	balanceResponse, err := logicSystem.GetBalanceLogic()
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
	user, err := ls.AuthenticateUser(w, r)
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
	logicSystem := service.LogicSystem{Ctx: r.Context(), Storage: ls.Storage, User: user}
	code, err := logicSystem.WithdrawLogic(withdrawRequest)

	if err != nil {
		http.Error(w, err.Error(), code)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (ls *ServerSystem) GetWithdrawalsHandler(w http.ResponseWriter, r *http.Request) {
	user, err := ls.AuthenticateUser(w, r)
	if err != nil {
		// Handle the error
		return
	}
	log.Printf("get withdraw call for %d\n", user.ID)

	// Получаем список выводов средств пользователя
	logicSystem := service.LogicSystem{Ctx: r.Context(), Storage: ls.Storage, User: user}
	withdrawalResponses, err := logicSystem.GetWithdrawalsLogic()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Если список выводов пуст, возвращаем 204 No Content
	if len(withdrawalResponses) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Возвращаем список выводов в формате JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(withdrawalResponses)
}

// AuthenticateUser authenticates the user and looks up the user in the database.
func (ls *ServerSystem) AuthenticateUser(w http.ResponseWriter, r *http.Request) (*dbconnector.User, error) {
	// Проверяем аутентификацию пользователя
	cookie, err := r.Cookie("session_token")
	if err != nil {
		http.Error(w, "User not authenticated", http.StatusUnauthorized)
		return &dbconnector.User{}, err
	}

	// Ищем пользователя в базе данных
	user, err := ls.Storage.GetUserByEmail(r.Context(), cookie.Value)
	if err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return &user, err
	}

	return &user, nil
}
