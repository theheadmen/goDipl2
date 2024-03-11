package errors

import "fmt"

var (
	ErrAlreadyHaveOrder             = fmt.Errorf("we already have order")
	ErrAlreadyHaveOrderForOtherUser = fmt.Errorf("already have order for other user")
	ErrInsufficientFunds            = fmt.Errorf("insufficient funds")
	ErrInvalidOrderNumber           = fmt.Errorf("invalid order number format")
)
