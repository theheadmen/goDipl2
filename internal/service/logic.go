package service

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/theheadmen/goDipl2/internal/dbconnector"
	"github.com/theheadmen/goDipl2/internal/errors"
	"github.com/theheadmen/goDipl2/internal/models"
	"golang.org/x/crypto/bcrypt"
)

type LogicSystem struct {
	Ctx     context.Context
	Storage Storage
	User    *dbconnector.User
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

func (ls *LogicSystem) WithdrawLogic(withdrawRequest models.WithdrawRequest) (int /*httpCode*/, error) {
	// Создаем новый заказ на списание
	order := dbconnector.Order{
		Number: withdrawRequest.Order,
		UserID: ls.User.ID,
		Status: "PROCESSED", // Предполагаем, что списание сразу обрабатывается
	}
	// Создаем списание
	withdrawal := dbconnector.Withdrawal{
		Points: withdrawRequest.Sum,
		UserID: ls.User.ID,
		Number: withdrawRequest.Order,
	}

	var checkedUser dbconnector.User
	// отправляем Order, withdrawal и обновляем user - но в рамках одной транзакции
	err := ls.Storage.WithdrawalTransaction(ls.Ctx, &order, &withdrawal, &checkedUser, ls.User.Email, withdrawRequest.Sum)

	if err == errors.ErrInsufficientFunds {
		// отдельный код для недостатка средств
		log.Println("but user don't have enough money")
		return http.StatusPaymentRequired, err
	}

	return http.StatusInternalServerError, err // обычный код для ошибки
}

func (ls *LogicSystem) GetBalanceLogic() (models.BalanceResponse, error) {
	// Получаем сумму использованных баллов
	withdrawn := 0.0
	withdrawals, err := ls.Storage.GetAddWithdrawalsByUserID(ls.Ctx, ls.User.ID)
	if err != nil {
		return models.BalanceResponse{}, err
	}

	for _, withdrawal := range withdrawals {
		log.Printf("we have withdrawal with number %s, points %f\n", withdrawal.Number, withdrawal.Points)
		withdrawn += withdrawal.Points
	}

	log.Printf("get balance for ls.User %d, current %f, withdrawn %f\n", ls.User.ID, ls.User.Balance, withdrawn)

	// Формируем ответ
	balanceResponse := models.BalanceResponse{
		Current:   ls.User.Balance,
		Withdrawn: withdrawn,
	}

	return balanceResponse, nil
}

func (ls *LogicSystem) GetWithdrawalsLogic() ([]models.WithdrawalResponse, error) {
	// Получаем список выводов средств пользователя
	withdrawals, err := ls.Storage.GetAddWithdrawalsByUserID(ls.Ctx, ls.User.ID)
	if err != nil {
		//http.Error(w, err.Error(), http.StatusInternalServerError)
		return []models.WithdrawalResponse{}, err
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

	return withdrawalResponses, nil
}

func (ls *LogicSystem) GetOrderLogic() ([]models.OrderResponse, error) {
	// Получаем список заказов пользователя
	orders, err := ls.Storage.GetOrdersByUserID(ls.Ctx, ls.User.ID)
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

func (ls *LogicSystem) LoadOrderLogic(orderNumber string) error {
	// Проверяем корректность номера заказа
	if !IsValidLuhn(orderNumber) {
		log.Printf("For ls.User %d, get incorrect order: %s\n", ls.User.ID, orderNumber)
		return errors.ErrInvalidOrderNumber
	}

	log.Printf("For ls.User %d, get new order: %s\n", ls.User.ID, orderNumber)

	// Проверяем, не был ли загружен этот заказ другим пользователем
	isOrderExist, existingOrder, err := ls.Storage.GetOrderByNumber(ls.Ctx, orderNumber)
	if err != nil {
		return err
	}

	if isOrderExist {
		if existingOrder.UserID == ls.User.ID {
			log.Printf("For user %d, we already have order: %s\n", ls.User.ID, orderNumber)
			return errors.ErrAlreadyHaveOrder
		} else {
			log.Printf("For user %d, we can't add order: %s because we already have it for other user %d\n", ls.User.ID, orderNumber, existingOrder.UserID)
			return errors.ErrAlreadyHaveOrderForOtherUser
		}
	}

	// Создаем новый заказ
	order := dbconnector.Order{
		Number: orderNumber,
		UserID: ls.User.ID,
	}

	// Сохраняем заказ в базе данных
	err = ls.Storage.AddOrder(ls.Ctx, &order)

	return err
}

func (ls *LogicSystem) LoginUserLogic() (int /*responce code*/, error) {
	// Проверяем, что логин и пароль не пустые
	if ls.User.Email == "" || ls.User.Password == "" {
		log.Println("Login and password are required")
		return http.StatusBadRequest, fmt.Errorf("login and password are required")
	}

	// Ищем пользователя в базе данных
	checkedUser, err := ls.Storage.GetUserByEmail(ls.Ctx, ls.User.Email)
	if err != nil {
		log.Println("Invalid login or password")
		return http.StatusUnauthorized, fmt.Errorf("invalid login or password")
	}

	// Проверяем пароль
	err = bcrypt.CompareHashAndPassword([]byte(checkedUser.Password), []byte(ls.User.Password))
	if err != nil {
		log.Println("Invalid login or password")
		return http.StatusUnauthorized, fmt.Errorf("invalid login or password")
	}

	return 0, nil
}

func (ls *LogicSystem) RegisterUserLogic() (int /*responce code*/, error) {
	// Проверяем, что логин и пароль не пустые
	if ls.User.Email == "" || ls.User.Password == "" {
		return http.StatusBadRequest, fmt.Errorf("login and password are required")
	}

	// Хешируем пароль
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(ls.User.Password), bcrypt.DefaultCost)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	ls.User.Password = string(hashedPassword)

	// Сохраняем пользователя в базе данных
	err = ls.Storage.AddUser(ls.Ctx, ls.User)
	if err != nil {
		return http.StatusConflict, err
	}

	return 0, nil
}
