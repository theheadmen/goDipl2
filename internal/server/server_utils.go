package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/theheadmen/goDipl2/internal/dbconnector"
	"github.com/theheadmen/goDipl2/internal/models"
)

func FetchOrderInfo(db *dbconnector.DBConnector, ord *models.Order, baseURL string, ctx context.Context) error {
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
	err = db.UpdateOrder(ord, ctx)
	if err != nil {
		return fmt.Errorf("ошибка при обновлении записи в базе данных: %w", err)
	}

	// Если Accrual не пустое, обновляем Balance у User
	if orderResponse.Accrual > 0 {
		var user models.User
		err = db.GetUserByUserID(ord.UserID, &user, ctx)
		if err != nil {
			return fmt.Errorf("ошибка при поиске пользователя: %w", err)
		}

		// Обновляем Balance
		user.Balance += float64(orderResponse.Accrual)

		// Обновляем запись пользователя в базе данных
		err = db.UpdateUser(&user, ctx)
		if err != nil {
			return fmt.Errorf("ошибка при обновлении баланса пользователя: %w", err)
		}
		log.Printf("User %d now have %f\n", ord.UserID, user.Balance)
	}

	if ord.Status == "INVALID" || ord.Status == "PROCESSED" {
		return nil
	}

	return nil
}

func ProcessOrders(db *dbconnector.DBConnector, baseURL string, ctx context.Context) {
	var orders []models.Order
	// берем все заказы которые еще ждут выполнения
	err := db.GetWaitingOrders(&orders, ctx)
	if err != nil {
		fmt.Printf("ошибка при запросе ORDERS из бд: %+v", err)
	} else {
		for i := 0; i < len(orders); i++ {
			// и проверяем каждый
			ord := orders[i]
			err := FetchOrderInfo(db, &ord, baseURL, ctx)
			if err != nil {
				fmt.Printf("Ошибка при обработке заказа %s: %+v\n", ord.Number, err)
			}
		}
	}
}