package dbconnector

import (
	"context"
	"log"

	"github.com/theheadmen/goDipl2/internal/errors"
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

func (dbConnector *DBConnector) GetUserByEmail(ctx context.Context, email string) (User, error) {
	var checkedUser User
	result := dbConnector.DB.Where("email = ?", email).First(&checkedUser).WithContext(ctx)
	return checkedUser, result.Error
}

func (dbConnector *DBConnector) GetUserByUserID(ctx context.Context, userID uint) (User, error) {
	var user User
	result := dbConnector.DB.First(&user, userID).WithContext(ctx)
	return user, result.Error
}

func (dbConnector *DBConnector) GetOrderByNumber(ctx context.Context, orderNumber string) (bool, Order, error) {
	var existingOrder Order
	result := dbConnector.DB.Where("number = ?", orderNumber).First(&existingOrder).WithContext(ctx)

	if result.Error == nil {
		// мы нашли какой-то заказ с таким номером
		return true, existingOrder, nil
	} else {
		if result.Error == gorm.ErrRecordNotFound {
			// такой записи нет, мы не считаем это ошибкой
			return false, existingOrder, nil
		} else {
			// произошла какая-то другая ошибка, нужно обработать
			return false, existingOrder, result.Error
		}
	}
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

func (dbConnector *DBConnector) GetOrdersByUserID(ctx context.Context, userID uint) ([]Order, error) {
	var orders []Order
	result := dbConnector.DB.Where("user_id = ?", userID).Order("created_at").Find(&orders).WithContext(ctx)
	return orders, result.Error
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

func (dbConnector *DBConnector) GetAddWithdrawalsByUserID(ctx context.Context, userID uint) ([]Withdrawal, error) {
	var withdrawals []Withdrawal
	result := dbConnector.DB.Where("user_id = ? AND points > 0", userID).Order("created_at").Find(&withdrawals).WithContext(ctx)
	return withdrawals, result.Error
}

func (dbConnector *DBConnector) GetWaitingOrders(ctx context.Context) ([]Order, error) {
	var orders []Order
	result := dbConnector.DB.Where("status = 'REGISTERED' OR status = 'PROCESSING' OR status = 'NEW'").Find(&orders).WithContext(ctx)
	return orders, result.Error
}

func (dbConnector *DBConnector) WithdrawalTransaction(ctx context.Context, order *Order, withdrawal *Withdrawal, user *User, userEmail string, requestedSum float64) error {
	tx := dbConnector.DB.Begin()

	// мы знаем что такой пользователь есть, конкретно здесь нас интересует его баланс
	result := tx.Where("email = ?", userEmail).First(&user).WithContext(ctx)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}
	// мало денег - заканчиваем, возвращаем ошибку про средства
	if user.Balance < requestedSum {
		tx.Commit()
		return errors.ErrInsufficientFunds
	}
	// иначе обновляем баланс, и отправляем заказ и списание
	user.Balance -= requestedSum

	result = tx.Create(&order).WithContext(ctx)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	result = tx.Create(&withdrawal).WithContext(ctx)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	result = tx.Save(&user).WithContext(ctx)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	tx.Commit()
	return nil
}

func (dbConnector *DBConnector) DeleteAllData(ctx context.Context) error {
	tx := dbConnector.DB.Begin()

	// Delete all data from the Withdrawal table
	result := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Withdrawal{}).WithContext(ctx)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	// Delete all data from the Order table
	result = tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Order{}).WithContext(ctx)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	// Delete all data from the User table
	result = tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&User{}).WithContext(ctx)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	tx.Commit()
	log.Println("Clean data in database")
	return nil
}
