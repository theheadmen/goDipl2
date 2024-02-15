package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func (ls *LoyaltySystem) RegisterUserHandler(w http.ResponseWriter, r *http.Request) {
	var user User
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
	result := ls.DB.Create(&user).WithContext(r.Context())
	if result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusConflict)
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

func (ls *LoyaltySystem) LoginUserHandler(w http.ResponseWriter, r *http.Request) {
	var reqUser User
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
	var user User
	result := ls.DB.Where("email = ?", reqUser.Email).First(&user).WithContext(r.Context())
	if result.Error != nil {
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

func (ls *LoyaltySystem) LoadOrderHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяем аутентификацию пользователя
	cookie, err := r.Cookie("session_token")
	if err != nil {
		http.Error(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Ищем пользователя в базе данных
	var user User
	result := ls.DB.Where("email = ?", cookie.Value).First(&user).WithContext(r.Context())
	if result.Error != nil {
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
	var existingOrder Order
	result = ls.DB.Where("number = ?", orderNumber).First(&existingOrder).WithContext(r.Context())
	if result.Error == nil {
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
	order := Order{
		Number: orderNumber,
		UserID: user.ID,
	}

	// Сохраняем заказ в базе данных
	result = ls.DB.Create(&order).WithContext(r.Context())
	if result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}
	// отправляем заказ в канал на обработку
	ls.Datachan <- order

	log.Printf("For user %d, saved new order: %s\n", user.ID, orderNumber)

	w.WriteHeader(http.StatusAccepted)
}

func (ls *LoyaltySystem) GetOrdersHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяем аутентификацию пользователя
	cookie, err := r.Cookie("session_token")
	if err != nil {
		http.Error(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Ищем пользователя в базе данных
	var user User
	result := ls.DB.Where("email = ?", cookie.Value).First(&user).WithContext(r.Context())
	if result.Error != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}
	log.Printf("get orders call for %d\n", user.ID)

	// Получаем список заказов пользователя
	var orders []Order
	result = ls.DB.Where("user_id = ?", user.ID).Order("created_at").Find(&orders).WithContext(r.Context())
	if result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	// Если список заказов пуст, возвращаем 204 No Content
	if len(orders) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Конвертируем список заказов в список ответов
	orderResponses := make([]OrderResponse, len(orders))
	for i, order := range orders {
		orderResponses[i] = OrderResponse{
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

func (ls *LoyaltySystem) GetBalanceHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяем аутентификацию пользователя
	cookie, err := r.Cookie("session_token")
	if err != nil {
		http.Error(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Ищем пользователя в базе данных
	var user User
	result := ls.DB.Where("email = ?", cookie.Value).First(&user).WithContext(r.Context())
	if result.Error != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}
	log.Printf("get balance call for %d\n", user.ID)

	// Получаем сумму использованных баллов
	var withdrawn float64
	result = ls.DB.Model(&Withdrawal{}).
		Select("COALESCE(SUM(points), 0)").
		Where("user_id = ?", user.ID).
		Scan(&withdrawn).WithContext(r.Context())

	if result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	// Формируем ответ
	balanceResponse := BalanceResponse{
		Current:   user.Balance,
		Withdrawn: withdrawn,
	}

	// Возвращаем ответ в формате JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(balanceResponse)
}

func (ls *LoyaltySystem) WithdrawHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяем аутентификацию пользователя
	cookie, err := r.Cookie("session_token")
	if err != nil {
		http.Error(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Ищем пользователя в базе данных
	var user User
	result := ls.DB.Where("email = ?", cookie.Value).First(&user).WithContext(r.Context())
	if result.Error != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}
	log.Printf("withdraw call for %d\n", user.ID)

	// Декодируем JSON-запрос
	var withdrawRequest WithdrawRequest
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
	order := Order{
		Number: withdrawRequest.Order,
		UserID: user.ID,
		Status: "PROCESSED", // Предполагаем, что списание сразу обрабатывается
	}

	// Сохраняем заказ в базе данных
	result = ls.DB.Create(&order).WithContext(r.Context())
	if result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}
	// Создаем списание
	withdrawal := Withdrawal{
		Points: withdrawRequest.Sum,
		UserID: user.ID,
		Number: withdrawRequest.Order,
	}
	// Сохраняем списание в базе данных
	result = ls.DB.Create(&withdrawal).WithContext(r.Context())
	if result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	// Обновляем баланс пользователя
	user.Balance -= withdrawRequest.Sum
	result = ls.DB.Save(&user).WithContext(r.Context())
	if result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (ls *LoyaltySystem) GetWithdrawalsHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяем аутентификацию пользователя
	cookie, err := r.Cookie("session_token")
	if err != nil {
		http.Error(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Ищем пользователя в базе данных
	var user User
	result := ls.DB.Where("email = ?", cookie.Value).First(&user).WithContext(r.Context())
	if result.Error != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}
	log.Printf("get withdraw call for %d\n", user.ID)

	// Получаем список выводов средств пользователя
	var withdrawals []Withdrawal
	result = ls.DB.Where("user_id = ? AND points > 0", user.ID).Order("created_at").Find(&withdrawals).WithContext(r.Context())
	if result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	// Если список выводов пуст, возвращаем 204 No Content
	if len(withdrawals) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Конвертируем список выводов в список ответов
	withdrawalResponses := make([]WithdrawalResponse, len(withdrawals))
	for i, withdrawal := range withdrawals {
		withdrawalResponses[i] = WithdrawalResponse{
			Order:       withdrawal.Number,
			Sum:         withdrawal.Points,
			ProcessedAt: withdrawal.CreatedAt,
		}
	}

	// Возвращаем список выводов в формате JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(withdrawalResponses)
}

func fetchOrderInfo(db *gorm.DB, ord *Order, baseURL string, ctx context.Context) error {
	// Формируем URL запроса
	url := fmt.Sprintf("%s/api/orders/%s", baseURL, ord.Number)
	log.Printf("Try to fetch order: %s by url %s\n", ord.Number, url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("ошибка при составлении запроса: %w", err)
	}

	req = req.WithContext(ctx)
	// Отправляем GET-запрос
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("ошибка при отправке запроса: %w", err)
	}
	defer resp.Body.Close()

	// Проверяем код ответа
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("превышено количество запросов к сервису")
	}

	if resp.StatusCode == http.StatusInternalServerError {
		return fmt.Errorf("внутренняя ошибка сервера")
	}

	if resp.StatusCode == http.StatusNoContent {
		return fmt.Errorf("такого order нет для сервиса")
	}

	// Читаем тело ответа
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("ошибка при чтении тела ответа: %w", err)
	}

	// Декодируем JSON-ответ в структуру AccrualResponse
	var orderResponse AccrualResponse
	err = json.Unmarshal(body, &orderResponse)
	if err != nil {
		invalidJSON := string(body)
		return fmt.Errorf("ошибка при декодировании JSON: %w. Неправильный JSON: %s. Код ответа %d", err, invalidJSON, resp.StatusCode)
	}
	log.Printf("We get status %s and accrual %f\n", orderResponse.Status, orderResponse.Accrual)

	// Обновляем поля в Ord
	ord.Status = orderResponse.Status
	ord.Points = orderResponse.Accrual
	// Обновляем запись в базе данных
	err = db.Save(ord).WithContext(ctx).Error
	if err != nil {
		return fmt.Errorf("ошибка при обновлении записи в базе данных: %w", err)
	}

	// Если Accrual не пустое, обновляем Balance у User
	if orderResponse.Accrual > 0 {
		var user User
		err = db.First(&user, ord.UserID).WithContext(ctx).Error
		if err != nil {
			return fmt.Errorf("ошибка при поиске пользователя: %w", err)
		}

		// Обновляем Balance
		user.Balance += float64(orderResponse.Accrual)

		// Обновляем запись пользователя в базе данных
		err = db.Save(&user).WithContext(ctx).Error
		if err != nil {
			return fmt.Errorf("ошибка при обновлении баланса пользователя: %w", err)
		}
		log.Printf("User %d now have %f\n", ord.UserID, user.Balance)
	}

	if ord.Status == "INVALID" || ord.Status == "PROCESSED" {
		return nil
	}

	return nil
}

func ProcessOrders(db *gorm.DB, baseURL string, ctx context.Context) {
	var orders []Order
	// берем все заказы которые еще ждут выполнения
	result := db.Where("status = 'REGISTERED' OR status = 'PROCESSING'").Find(&orders).WithContext(ctx)
	if result.Error != nil {
		fmt.Printf("ошибка при запросе ORDERS из бд: %+v", result.Error)
	} else {
		for i := 0; i < len(orders); i++ {
			// и проверяем каждый
			ord := orders[i]
			err := fetchOrderInfo(db, &ord, baseURL, ctx)
			if err != nil {
				fmt.Printf("Ошибка при обработке заказа %s: %+v\n", ord.Number, err)
			}
		}
	}
}
