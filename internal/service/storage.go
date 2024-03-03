package service

import (
	"context"

	"github.com/theheadmen/goDipl2/internal/dbconnector"
)

type Storage interface {
	GetUserByEmail(ctx context.Context, email string, user *dbconnector.User) error
	GetUserByUserID(ctx context.Context, userID uint, user *dbconnector.User) error
	GetOrderByNumber(ctx context.Context, orderNumber string, existingOrder *dbconnector.Order) error
	AddOrder(ctx context.Context, newOrder *dbconnector.Order) error
	UpdateOrder(ctx context.Context, updOrder *dbconnector.Order) error
	AddUser(ctx context.Context, newUser *dbconnector.User) error
	UpdateUser(ctx context.Context, updUser *dbconnector.User) error
	DeleteUser(ctx context.Context, updUser *dbconnector.User) error
	GetOrdersByUserID(ctx context.Context, userID uint, orders *[]dbconnector.Order) error
	AddWithdrawal(ctx context.Context, withdrawal *dbconnector.Withdrawal) error
	GetAddWithdrawalsByUserID(ctx context.Context, userID uint, withdrawals *[]dbconnector.Withdrawal) error
	GetWaitingOrders(ctx context.Context, orders *[]dbconnector.Order) error
	WithdrawalTransaction(ctx context.Context, order *dbconnector.Order, withdrawal *dbconnector.Withdrawal, user *dbconnector.User, userEmail string, requestedSum float64, fundError error) error
}
