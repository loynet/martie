package app

import (
	"context"

	"martie/internal/telegram"
)

type messageSender interface {
	Send(context.Context, telegram.SendRequest) error
}
