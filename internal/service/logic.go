package service

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/theheadmen/goDipl2/internal/dbconnector"
	"github.com/theheadmen/goDipl2/internal/models"
	"golang.org/x/crypto/bcrypt"
)

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

func WithdrawLogic(ctx context.Context, storage Storage, userEmail string, userID uint, withdrawRequest models.WithdrawRequest) (int /*httpCode*/, error) {
	// Создаем новый заказ на списание
	order := dbconnector.Order{
		Number: withdrawRequest.Order,
		UserID: userID,
		Status: "PROCESSED", // Предполагаем, что списание сразу обрабатывается
	}
	// Создаем списание
	withdrawal := dbconnector.Withdrawal{
		Points: withdrawRequest.Sum,
		UserID: userID,
		Number: withdrawRequest.Order,
	}

	fundError := fmt.Errorf("insufficient funds")
	var user dbconnector.User
	// отправляем Order, withdrawal и обновляем user - но в рамках одной транзакции
	err := storage.WithdrawalTransaction(ctx, &order, &withdrawal, &user, userEmail, withdrawRequest.Sum, fundError)

	if err == fundError {
		// отдельный код для недостатка средств
		log.Println("but user don't have enough money")
		return http.StatusPaymentRequired, fundError
	}

	return http.StatusInternalServerError, err // обычный код для ошибки
}

func GetBalanceLogic(ctx context.Context, storage Storage, user dbconnector.User) (models.BalanceResponse, error) {
	// Получаем сумму использованных баллов
	withdrawn := 0.0
	var withdrawals []dbconnector.Withdrawal
	err := storage.GetAddWithdrawalsByUserID(ctx, user.ID, &withdrawals)
	if err != nil {
		return models.BalanceResponse{}, err
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

	return balanceResponse, nil
}

func GetOrderLogic(ctx context.Context, storage Storage, user dbconnector.User) ([]models.OrderResponse, error) {
	// Получаем список заказов пользователя
	var orders []dbconnector.Order
	err := storage.GetOrdersByUserID(ctx, user.ID, &orders)
	if err != nil {
		return []models.OrderResponse{}, err
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

	return orderResponses, nil
}

func LoadOrderLogic(ctx context.Context, orderNumber string, storage Storage, w http.ResponseWriter, user dbconnector.User) error {
	// Проверяем корректность номера заказа
	if !IsValidLuhn(orderNumber) {
		log.Printf("For user %d, get incorrect order: %s\n", user.ID, orderNumber)
		http.Error(w, "Invalid order number format", http.StatusUnprocessableEntity)
		return fmt.Errorf("invalid order number format")
	}

	log.Printf("For user %d, get new order: %s\n", user.ID, orderNumber)

	// Проверяем, не был ли загружен этот заказ другим пользователем
	var existingOrder dbconnector.Order
	err := storage.GetOrderByNumber(ctx, orderNumber, &existingOrder)
	if err == nil {
		if existingOrder.UserID == user.ID {
			log.Printf("For user %d, we already have order: %s\n", user.ID, orderNumber)
			w.WriteHeader(http.StatusOK)
			return fmt.Errorf("we already have order")
		} else {
			log.Printf("For user %d, we can't add order: %s because we already have it for other user %d\n", user.ID, orderNumber, existingOrder.UserID)
			w.WriteHeader(http.StatusConflict)
			return fmt.Errorf("already have order for other user")
		}
	}

	// Создаем новый заказ
	order := dbconnector.Order{
		Number: orderNumber,
		UserID: user.ID,
	}

	// Сохраняем заказ в базе данных
	err = storage.AddOrder(ctx, &order)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return err
	}

	return nil
}

func LoginUserLogic(ctx context.Context, storage Storage, reqUser dbconnector.User) (int /*responce code*/, error) {
	// Проверяем, что логин и пароль не пустые
	if reqUser.Email == "" || reqUser.Password == "" {
		log.Println("Login and password are required")
		return http.StatusBadRequest, fmt.Errorf("login and password are required")
	}

	// Ищем пользователя в базе данных
	var user dbconnector.User
	err := storage.GetUserByEmail(ctx, reqUser.Email, &user)
	if err != nil {
		log.Println("Invalid login or password")
		return http.StatusUnauthorized, fmt.Errorf("invalid login or password")
	}

	// Проверяем пароль
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(reqUser.Password))
	if err != nil {
		log.Println("Invalid login or password")
		return http.StatusUnauthorized, fmt.Errorf("invalid login or password")
	}

	return 0, nil
}

func RegisterUserLogic(ctx context.Context, storage Storage, user dbconnector.User) (int /*responce code*/, error) {
	// Проверяем, что логин и пароль не пустые
	if user.Email == "" || user.Password == "" {
		return http.StatusBadRequest, fmt.Errorf("login and password are required")
	}

	// Хешируем пароль
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	user.Password = string(hashedPassword)

	// Сохраняем пользователя в базе данных
	err = storage.AddUser(ctx, &user)
	if err != nil {
		return http.StatusConflict, err
	}

	return 0, nil
}
