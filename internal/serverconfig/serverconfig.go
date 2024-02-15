package serverconfig

import (
	"flag"
	"os"
)

type ConfigStore struct {
	FlagRunAddr  string
	FlagDatabase string
	FlagAccrual  string
}

func NewConfigStore() *ConfigStore {
	return &ConfigStore{
		FlagRunAddr:  "",
		FlagDatabase: "",
		FlagAccrual:  "",
	}
}

// parseFlags обрабатывает аргументы командной строки
// и сохраняет их значения в соответствующих переменных
func (configStore *ConfigStore) ParseFlags() {
	// регистрируем переменную flagRunAddr
	// как аргумент -a со значением :8080 по умолчанию
	flag.StringVar(&configStore.FlagRunAddr, "a", ":8080", "address and port to run server")
	flag.StringVar(&configStore.FlagDatabase, "d", "", "data for connecting to db")
	flag.StringVar(&configStore.FlagAccrual, "r", "", "accrual service url")
	// парсим переданные серверу аргументы в зарегистрированные переменные
	flag.Parse()

	if envRunAddr := os.Getenv("RUN_ADDRESS"); envRunAddr != "" {
		configStore.FlagRunAddr = envRunAddr
	}

	if envShortRunAddr := os.Getenv("DATABASE_URI"); envShortRunAddr != "" {
		configStore.FlagDatabase = envShortRunAddr
	}

	if envLogLevel := os.Getenv("ACCRUAL_SYSTEM_ADDRESS"); envLogLevel != "" {
		configStore.FlagAccrual = envLogLevel
	}
}
