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
	"github.com/theheadmen/goDipl2/internal/service"
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

func fetchOrderInfo(ctx context.Context, storage service.Storage, ord *dbconnector.Order, baseURL string, defTimeToReturn int) (int, error) {
	// Формируем URL запроса
	url := fmt.Sprintf("%s/api/orders/%s", baseURL, ord.Number)
	log.Printf("Try to fetch order: %s by url %s\n", ord.Number, url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return defTimeToReturn, fmt.Errorf("ошибка при составлении запроса: %w", err)
	}

	req = req.WithContext(ctx)
	// Отправляем GET-запрос
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return defTimeToReturn, fmt.Errorf("ошибка при отправке запроса: %w", err)
	}
	defer resp.Body.Close()

	// Проверяем код ответа
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		retryAfterSeconds, err := strconv.Atoi(retryAfter)
		if err != nil {
			return defTimeToReturn, err
		}
		log.Println("Сервис попросил повторить запрос через", retryAfterSeconds, "секунд")
		return retryAfterSeconds, fmt.Errorf("превышено количество запросов к сервису")
	}

	if resp.StatusCode == http.StatusInternalServerError {
		return defTimeToReturn, fmt.Errorf("внутренняя ошибка сервера")
	}

	if resp.StatusCode == http.StatusNoContent {
		return defTimeToReturn, fmt.Errorf("такого order нет для сервиса")
	}

	// Читаем тело ответа
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return defTimeToReturn, fmt.Errorf("ошибка при чтении тела ответа: %w", err)
	}

	// Декодируем JSON-ответ в структуру AccrualResponse
	var orderResponse models.AccrualResponse
	err = json.Unmarshal(body, &orderResponse)
	if err != nil {
		invalidJSON := string(body)
		return defTimeToReturn, fmt.Errorf("ошибка при декодировании JSON: %w. Неправильный JSON: %s. Код ответа %d", err, invalidJSON, resp.StatusCode)
	}
	log.Printf("We get status %s and accrual %f\n", orderResponse.Status, orderResponse.Accrual)

	// Обновляем поля в Ord
	ord.Status = orderResponse.Status
	ord.Points = orderResponse.Accrual
	// Обновляем запись в базе данных
	err = storage.UpdateOrder(ctx, ord)
	if err != nil {
		return defTimeToReturn, fmt.Errorf("ошибка при обновлении записи в базе данных: %w", err)
	}

	// Если Accrual не пустое, обновляем Balance у User
	if orderResponse.Accrual > 0 {
		var user dbconnector.User
		err = storage.GetUserByUserID(ctx, ord.UserID, &user)
		if err != nil {
			return defTimeToReturn, fmt.Errorf("ошибка при поиске пользователя: %w", err)
		}

		// Обновляем Balance
		user.Balance += float64(orderResponse.Accrual)

		// Обновляем запись пользователя в базе данных
		err = storage.UpdateUser(ctx, &user)
		if err != nil {
			return defTimeToReturn, fmt.Errorf("ошибка при обновлении баланса пользователя: %w", err)
		}
		log.Printf("User %d now have %f\n", ord.UserID, user.Balance)
	}

	/*if ord.Status == "INVALID" || ord.Status == "PROCESSED" {
		// нужна ли здесь логика?
		return nil
	}*/

	return defTimeToReturn, nil
}

func processOrders(ctx context.Context, storage service.Storage, baseURL string, defTimeToReturn int) time.Duration {
	var orders []dbconnector.Order
	// берем все заказы которые еще ждут выполнения
	err := storage.GetWaitingOrders(ctx, &orders)
	if err != nil {
		fmt.Printf("ошибка при запросе ORDERS из бд: %+v", err)
		return time.Duration(defTimeToReturn) * time.Second
	}

	for i := 0; i < len(orders); i++ {
		// и проверяем каждый
		ord := orders[i]
		timeToResetTimer, err := fetchOrderInfo(ctx, storage, &ord, baseURL, defTimeToReturn)
		if timeToResetTimer != defTimeToReturn {
			// если вернулось не время по умолчанию, значит был Retry-After ответ
			// мы можем не продолжать обрабатывать остальные заказы, сразу возвращаем время которое нас попросили подождать
			return time.Duration(timeToResetTimer) * time.Second
		}
		if err != nil {
			// в целом, проблема с одним заказом еще не повод не рассчитать остальные
			fmt.Printf("Ошибка при обработке заказа %s: %+v\n", ord.Number, err)
		}
	}

	return time.Duration(defTimeToReturn) * time.Second
}

func MakeGorutineToCheckOrdersByTimer(ctx context.Context, ls *ServerSystem) {
	// если случилась ошибка не связанная с Retry-After - возвращаем время по умолчанию
	defTimeToReturn := 3

	go func() {
		ctx2 := context.Background()
		defTimerTime := time.Duration(defTimeToReturn) * time.Second
		ticker := time.NewTicker(defTimerTime)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Println("Время проверить заказы по таймеру, заодно останавливаем таймер")
				// мы хотим дождаться обработки всех текущих заказов, прежде чем обработать новые
				ticker.Stop()
				newTimeForTimer := processOrders(ctx2, ls.Storage, ls.BaseURL, defTimeToReturn)
				// если случилась ошибка с Retry After, здесь будет время которое просил подождать сервер
				// в ином случае - стандартное наше время ожидания
				ticker.Reset(newTimeForTimer)
			}
		}
	}()
}
