package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/theheadmen/goDipl2/internal/models"
	"github.com/theheadmen/goDipl2/internal/server"
	"github.com/theheadmen/goDipl2/internal/serverconfig"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	configStore := serverconfig.NewConfigStore()
	configStore.ParseFlags()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dsn := configStore.FlagDatabase
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Канал для получения данных
	dataChan := make(chan models.Order)

	ls := server.NewServerSystem(db, dataChan, configStore.FlagAccrual)
	if err := ls.DBInitialize(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	srv := ls.MakeServer(configStore.FlagRunAddr)

	// Горутина, которая ждет данных из канала
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case data := <-dataChan:
				log.Println("New data to check in channel!")
				ctx2 := context.Background()
				err := server.FetchOrderInfo(ls.DB, &data, ls.BaseURL, ctx2)
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
				server.ProcessOrders(ls.DB, ls.BaseURL, ctx2)
			}
		}
	}()

	go func() {
		log.Printf("Starting server on %s\n", configStore.FlagRunAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Ожидание сигнала завершения
	<-ctx.Done()
}
