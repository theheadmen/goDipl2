package main

import (
	"gorm.io/gorm"
)

type LoyaltySystem struct {
	DB       *gorm.DB
	Datachan chan Order
	BaseURL  string
}

func NewLoyaltySystem(db *gorm.DB, datachan chan Order, baseURL string) *LoyaltySystem {
	return &LoyaltySystem{DB: db, Datachan: datachan, BaseURL: baseURL}
}

func (ls *LoyaltySystem) Initialize() error {
	return ls.DB.AutoMigrate(&User{}, &Order{}, &Withdrawal{})
}
