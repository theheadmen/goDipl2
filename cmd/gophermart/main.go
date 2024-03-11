package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/theheadmen/goDipl2/internal/dbconnector"
	"github.com/theheadmen/goDipl2/internal/server"
	"github.com/theheadmen/goDipl2/internal/serverconfig"
)

func main() {
	configStore := serverconfig.NewConfigStore()
	configStore.ParseFlags()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := dbconnector.OpenDBConnect(configStore.FlagDatabase)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	if err := db.DBInitialize(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	ls := server.NewServerSystem(db, configStore.FlagAccrual)
	srv := ls.MakeServer(configStore.FlagRunAddr)

	// Горутина, которая выполняет проверяет orders раз в 30 секунд
	server.MakeGorutineToCheckOrdersByTimer(ctx, ls)

	go func() {
		log.Printf("Starting server on %s\n", configStore.FlagRunAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Ожидание сигнала завершения
	<-ctx.Done()
}
