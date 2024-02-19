package dbconnector

import (
	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Email    string  `json:"login" gorm:"unique;not null"`
	Password string  `json:"password" gorm:"not null"`
	Balance  float64 `gorm:"default:0"`
}

type Order struct {
	gorm.Model
	Number string  `gorm:"unique;not null"`
	Status string  `gorm:"default:'NEW'"`
	Points float64 `gorm:"default:0"`
	UserID uint
	User   User
}

type Withdrawal struct {
	gorm.Model
	Points float64 `gorm:"default:0"`
	UserID uint    `gorm:"not null"`
	Number string  `gorm:"not null"`
}
