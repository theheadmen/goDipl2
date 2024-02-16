package dbconnector

import (
	"context"

	"github.com/theheadmen/goDipl2/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type DBConnector struct {
	DB *gorm.DB
}

func OpenDbConnect(dsn string) (*DBConnector, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	return &DBConnector{DB: db}, err
}

func (dbConnector *DBConnector) DBInitialize() error {
	return dbConnector.DB.AutoMigrate(&models.User{}, &models.Order{}, &models.Withdrawal{})
}

func (dbConnector *DBConnector) GetUserByEmail(email string, user *models.User, ctx context.Context) error {
	result := dbConnector.DB.Where("email = ?", email).First(&user).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) GetUserByUserID(userID uint, user *models.User, ctx context.Context) error {
	result := dbConnector.DB.First(&user, userID).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) GetOrderByNumber(orderNumber string, existingOrder *models.Order, ctx context.Context) error {
	result := dbConnector.DB.Where("number = ?", orderNumber).First(&existingOrder).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) AddOrder(newOrder *models.Order, ctx context.Context) error {
	result := dbConnector.DB.Create(&newOrder).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) UpdateOrder(updOrder *models.Order, ctx context.Context) error {
	result := dbConnector.DB.Save(&updOrder).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) AddUser(newUser *models.User, ctx context.Context) error {
	result := dbConnector.DB.Create(&newUser).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) UpdateUser(updUser *models.User, ctx context.Context) error {
	result := dbConnector.DB.Save(&updUser).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) DeleteUser(updUser *models.User, ctx context.Context) error {
	result := dbConnector.DB.Delete(&updUser).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) GetOrdersByUserID(userID uint, orders *[]models.Order, ctx context.Context) error {
	result := dbConnector.DB.Where("user_id = ?", userID).Order("created_at").Find(&orders).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) GetSumOfWithdrawalByUserID(userID uint, withdrawn *float64, ctx context.Context) error {
	result := dbConnector.DB.Model(&models.Withdrawal{}).
		Select("COALESCE(SUM(points), 0)").
		Where("user_id = ?", userID).
		Scan(&withdrawn).WithContext(ctx)

	return result.Error
}

func (dbConnector *DBConnector) AddWithdrawal(withdrawal *models.Withdrawal, ctx context.Context) error {
	result := dbConnector.DB.Create(&withdrawal).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) GetAddWithdrawalsByUserID(userID uint, withdrawals *[]models.Withdrawal, ctx context.Context) error {
	result := dbConnector.DB.Where("user_id = ? AND points > 0", userID).Order("created_at").Find(&withdrawals).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) GetWaitingOrders(orders *[]models.Order, ctx context.Context) error {
	result := dbConnector.DB.Where("status = 'REGISTERED' OR status = 'PROCESSING'").Find(&orders).WithContext(ctx)
	return result.Error
}
