package service

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/theheadmen/goDipl2/internal/dbconnector"
	"github.com/theheadmen/goDipl2/internal/models"
)

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
