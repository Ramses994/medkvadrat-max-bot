package handlers

import (
	"context"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
)

type maxAPI interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
	AnswerCallback(ctx context.Context, callbackID, text string) error
	AnswerCallbackReplace(ctx context.Context, callbackID, notification, text string) error
	SendMessageWithKeyboard(ctx context.Context, userID int64, cold bool, text string, rows [][]maxclient.CallbackButton) error
}

type gatewayAPI interface {
	SearchByPhone(ctx context.Context, phone string) ([]gateway.Patient, error)
	GetLabPanels(ctx context.Context, patientID, daysBack int) ([]gateway.LabPanel, error)
	PostConfirmation(ctx context.Context, planningID int64, status string, patientID int64, apptVersion string) (stored string, already, stale bool, err error)
}
