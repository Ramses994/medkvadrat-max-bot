package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// HTTPError is a non-2xx gateway response with optional business code.
type HTTPError struct {
	StatusCode int
	Code       string
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("gateway %d (%s): %s", e.StatusCode, e.Code, e.Body)
	}
	return fmt.Sprintf("gateway %d: %s", e.StatusCode, e.Body)
}

func (e *HTTPError) IsNotFound() bool   { return e.StatusCode == http.StatusNotFound }
func (e *HTTPError) IsForbidden() bool { return e.StatusCode == http.StatusForbidden }

type confirmationBody struct {
	PlanningID int64  `json:"planning_id"`
	PatientID  int64  `json:"patient_id"`
	Status     string `json:"status"`
	Source     string `json:"source"`
}

// PostConfirmation records patient response (POST /api/internal/confirmations).
func (c *Client) PostConfirmation(ctx context.Context, planningID int64, status string, patientID int64) error {
	body, err := json.Marshal(confirmationBody{
		PlanningID: planningID,
		PatientID:  patientID,
		Status:     status,
		Source:     "max",
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/internal/confirmations", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	httpErr := &HTTPError{StatusCode: resp.StatusCode, Body: string(data)}
	var api apiResponse
	if err := json.Unmarshal(data, &api); err == nil {
		if api.ErrorDetails != nil {
			httpErr.Code = api.ErrorDetails.Code
		}
		if api.Error != "" && httpErr.Body == "" {
			httpErr.Body = api.Error
		}
	}
	return httpErr
}
