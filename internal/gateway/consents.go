package gateway

import (
	"context"
	"net/url"
	"strconv"
)

type ConsentView struct {
	MessengerNotify *int
	Service         string
	Ads             string
	ServiceVersion  string
	AdsVersion      string
	ServiceAt       string
	AdsAt           string
}

type consentWire struct {
	MessengerNotify *int `json:"messenger_notify"`
	Service         struct {
		State       string `json:"state"`
		TextVersion string `json:"text_version"`
		At          string `json:"at"`
	} `json:"service"`
	Ads struct {
		State       string `json:"state"`
		TextVersion string `json:"text_version"`
		At          string `json:"at"`
	} `json:"ads"`
}

func (c *Client) GetConsent(ctx context.Context, patientID int64) (ConsentView, error) {
	q := url.Values{}
	q.Set("patient_id", strconv.FormatInt(patientID, 10))
	var wire consentWire
	if err := c.get(ctx, "/api/internal/bot-consents?"+q.Encode(), &wire); err != nil {
		return ConsentView{}, err
	}
	return ConsentView{
		MessengerNotify: wire.MessengerNotify,
		Service:         wire.Service.State,
		Ads:             wire.Ads.State,
		ServiceVersion:  wire.Service.TextVersion,
		AdsVersion:      wire.Ads.TextVersion,
		ServiceAt:       wire.Service.At,
		AdsAt:           wire.Ads.At,
	}, nil
}

func (c *Client) PostConsent(ctx context.Context, patientID, maxUserID int64, kind, version, action string) error {
	body := map[string]any{
		"patient_id":   patientID,
		"max_user_id":  maxUserID,
		"kind":         kind,
		"text_version": version,
		"action":       action,
	}
	var wire consentWire
	return c.postJSON(ctx, "bot-consents", "/api/internal/bot-consents", body, &wire)
}
