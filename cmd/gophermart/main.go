package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	configStore := NewConfigStore()
	configStore.ParseFlags()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dsn := configStore.FlagDatabase
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Канал для получения данных
	dataChan := make(chan Order)

	ls := NewLoyaltySystem(db, dataChan, configStore.FlagAccrual)
	if err := ls.Initialize(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	r := mux.NewRouter()
	r.HandleFunc("/api/user/register", ls.RegisterUserHandler).Methods("POST")
	r.HandleFunc("/api/user/login", ls.LoginUserHandler).Methods("POST")
	r.HandleFunc("/api/user/orders", ls.LoadOrderHandler).Methods("POST")
	r.HandleFunc("/api/user/orders", ls.GetOrdersHandler).Methods("GET")
	r.HandleFunc("/api/user/balance", ls.GetBalanceHandler).Methods("GET")
	r.HandleFunc("/api/user/balance/withdraw", ls.WithdrawHandler).Methods("POST")
	r.HandleFunc("/api/user/withdrawals", ls.GetWithdrawalsHandler).Methods("GET")

	server := &http.Server{
		Addr:    configStore.FlagRunAddr,
		Handler: r,
		// Good practice: enforce timeouts for servers you create!
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	// Горутина, которая ждет данных из канала
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case data := <-dataChan:
				log.Println("New data to check in channel!")
				ctx2 := context.Background()
				err := fetchOrderInfo(ls.DB, &data, ls.BaseURL, ctx2)
				if err != nil {
					log.Printf("For user %d, failed to check order: %d, error %+v\n", data.UserID, data.ID, err)
				}
			}
		}
	}()

	// Горутина, которая выполняет ProcessOrders() раз в 30 секунд
	go func() {
		ctx2 := context.Background()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Println("Time to check orders by timer")
				ProcessOrders(ls.DB, ls.BaseURL, ctx2)
			}
		}
	}()

	go func() {
		log.Printf("Starting server on %s\n", configStore.FlagRunAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Ожидание сигнала завершения
	<-ctx.Done()
}
