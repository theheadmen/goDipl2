package models

import (
	"time"

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

type OrderResponse struct {
	Number     string    `json:"number"`
	Status     string    `json:"status"`
	Accrual    float64   `json:"accrual,omitempty"`
	UploadedAt time.Time `json:"uploaded_at"`
}

type BalanceResponse struct {
	Current   float64 `json:"current"`
	Withdrawn float64 `json:"withdrawn"`
}

type WithdrawRequest struct {
	Order string  `json:"order"`
	Sum   float64 `json:"sum"`
}

type WithdrawalResponse struct {
	Order       string    `json:"order"`
	Sum         float64   `json:"sum"`
	ProcessedAt time.Time `json:"processed_at"`
}

type AccrualResponse struct {
	Order   string  `json:"order"`
	Status  string  `json:"status"`
	Accrual float64 `json:"accrual,omitempty"`
}
