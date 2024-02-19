package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/theheadmen/goDipl2/internal/dbconnector"
	"github.com/theheadmen/goDipl2/internal/models"
)

func IsValidLuhn(number string) bool {
	digits := len(number)
	parity := digits % 2
	sum := 0
	for i := 0; i < digits; i++ {
		digit, err := strconv.Atoi(string(number[i]))
		if err != nil {
			return false // Если символ не является цифрой, возвращаем false
		}
		if i%2 == parity {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	return sum%10 == 0
}

func fetchOrderInfo(db *dbconnector.DBConnector, ord *dbconnector.Order, baseURL string, ctx context.Context) error {
	// Формируем URL запроса
	url := fmt.Sprintf("%s/api/orders/%s", baseURL, ord.Number)
	log.Printf("Try to fetch order: %s by url %s\n", ord.Number, url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("ошибка при составлении запроса: %w", err)
	}

	req = req.WithContext(ctx)
	// Отправляем GET-запрос
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("ошибка при отправке запроса: %w", err)
	}
	defer resp.Body.Close()

	// Проверяем код ответа
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("превышено количество запросов к сервису")
	}

	if resp.StatusCode == http.StatusInternalServerError {
		return fmt.Errorf("внутренняя ошибка сервера")
	}

	if resp.StatusCode == http.StatusNoContent {
		return fmt.Errorf("такого order нет для сервиса")
	}

	// Читаем тело ответа
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("ошибка при чтении тела ответа: %w", err)
	}

	// Декодируем JSON-ответ в структуру AccrualResponse
	var orderResponse models.AccrualResponse
	err = json.Unmarshal(body, &orderResponse)
	if err != nil {
		invalidJSON := string(body)
		return fmt.Errorf("ошибка при декодировании JSON: %w. Неправильный JSON: %s. Код ответа %d", err, invalidJSON, resp.StatusCode)
	}
	log.Printf("We get status %s and accrual %f\n", orderResponse.Status, orderResponse.Accrual)

	// Обновляем поля в Ord
	ord.Status = orderResponse.Status
	ord.Points = orderResponse.Accrual
	// Обновляем запись в базе данных
	err = db.UpdateOrder(ctx, ord)
	if err != nil {
		return fmt.Errorf("ошибка при обновлении записи в базе данных: %w", err)
	}

	// Если Accrual не пустое, обновляем Balance у User
	if orderResponse.Accrual > 0 {
		var user dbconnector.User
		err = db.GetUserByUserID(ctx, ord.UserID, &user)
		if err != nil {
			return fmt.Errorf("ошибка при поиске пользователя: %w", err)
		}

		// Обновляем Balance
		user.Balance += float64(orderResponse.Accrual)

		// Обновляем запись пользователя в базе данных
		err = db.UpdateUser(ctx, &user)
		if err != nil {
			return fmt.Errorf("ошибка при обновлении баланса пользователя: %w", err)
		}
		log.Printf("User %d now have %f\n", ord.UserID, user.Balance)
	}

	/*if ord.Status == "INVALID" || ord.Status == "PROCESSED" {
		// нужна ли здесь логика?
		return nil
	}*/

	return nil
}

func processOrders(db *dbconnector.DBConnector, baseURL string, ctx context.Context) {
	var orders []dbconnector.Order
	// берем все заказы которые еще ждут выполнения
	err := db.GetWaitingOrders(ctx, &orders)
	if err != nil {
		fmt.Printf("ошибка при запросе ORDERS из бд: %+v", err)
	} else {
		for i := 0; i < len(orders); i++ {
			// и проверяем каждый
			ord := orders[i]
			err := fetchOrderInfo(db, &ord, baseURL, ctx)
			if err != nil {
				fmt.Printf("Ошибка при обработке заказа %s: %+v\n", ord.Number, err)
			}
		}
	}
}

func MakeGorutineToCheckOrder(ctx context.Context, ls *ServerSystem) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case data := <-ls.Datachan:
				log.Println("New data to check in channel!")
				ctx2 := context.Background()
				err := fetchOrderInfo(ls.DB, &data, ls.BaseURL, ctx2)
				if err != nil {
					log.Printf("For user %d, failed to check order: %d, error %+v\n", data.UserID, data.ID, err)
				}
			}
		}
	}()
}

func MakeGorutineToCheckOrdersByTimer(ctx context.Context, ls *ServerSystem) {
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
				processOrders(ls.DB, ls.BaseURL, ctx2)
			}
		}
	}()
}
