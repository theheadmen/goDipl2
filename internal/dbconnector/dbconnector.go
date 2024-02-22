package dbconnector

import (
	"context"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type DBConnector struct {
	DB *gorm.DB
}

func OpenDBConnect(dsn string) (*DBConnector, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	return &DBConnector{DB: db}, err
}

func (dbConnector *DBConnector) DBInitialize() error {
	return dbConnector.DB.AutoMigrate(&User{}, &Order{}, &Withdrawal{})
}

func (dbConnector *DBConnector) GetUserByEmail(ctx context.Context, email string, user *User) error {
	result := dbConnector.DB.Where("email = ?", email).First(&user).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) GetUserByUserID(ctx context.Context, userID uint, user *User) error {
	result := dbConnector.DB.First(&user, userID).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) GetOrderByNumber(ctx context.Context, orderNumber string, existingOrder *Order) error {
	result := dbConnector.DB.Where("number = ?", orderNumber).First(&existingOrder).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) AddOrder(ctx context.Context, newOrder *Order) error {
	result := dbConnector.DB.Create(&newOrder).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) UpdateOrder(ctx context.Context, updOrder *Order) error {
	result := dbConnector.DB.Save(&updOrder).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) AddUser(ctx context.Context, newUser *User) error {
	result := dbConnector.DB.Create(&newUser).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) UpdateUser(ctx context.Context, updUser *User) error {
	result := dbConnector.DB.Save(&updUser).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) DeleteUser(ctx context.Context, updUser *User) error {
	result := dbConnector.DB.Delete(&updUser).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) GetOrdersByUserID(ctx context.Context, userID uint, orders *[]Order) error {
	result := dbConnector.DB.Where("user_id = ?", userID).Order("created_at").Find(&orders).WithContext(ctx)
	return result.Error
}

/*func (dbConnector *DBConnector) GetSumOfWithdrawalByUserID(userID uint, withdrawn *float64) error {
	result := dbConnector.DB.Model(&Withdrawal{}).
		Select("COALESCE(SUM(points), 0)").
		Where("user_id = ?", userID).
		Scan(&withdrawn).WithContext(ctx)

	return result.Error
}*/

func (dbConnector *DBConnector) AddWithdrawal(ctx context.Context, withdrawal *Withdrawal) error {
	result := dbConnector.DB.Create(&withdrawal).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) GetAddWithdrawalsByUserID(ctx context.Context, userID uint, withdrawals *[]Withdrawal) error {
	result := dbConnector.DB.Where("user_id = ? AND points > 0", userID).Order("created_at").Find(&withdrawals).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) GetWaitingOrders(ctx context.Context, orders *[]Order) error {
	result := dbConnector.DB.Where("status = 'REGISTERED' OR status = 'PROCESSING' OR status = 'NEW'").Find(&orders).WithContext(ctx)
	return result.Error
}

func (dbConnector *DBConnector) WithdrawalTransaction(ctx context.Context, order *Order, withdrawal *Withdrawal, updUser *User) error {
	tx := dbConnector.DB.Begin()

	result := tx.Create(&order).WithContext(ctx)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	result = tx.Create(&withdrawal).WithContext(ctx)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	result = tx.Save(&updUser).WithContext(ctx)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	tx.Commit()
	return nil
}
