package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client — клиент к api-gateway (Go-сервис из другого репозитория,
// слушает на :8080 и ходит в MSSQL Medialog).
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// ===== Типы, совместимые с api-gateway =====

type apiResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

type Patient struct {
	PatientID int    `json:"patient_id"`
	FullName  string `json:"full_name"`
	Phone     string `json:"phone"`
	BirthDate string `json:"birth_date"`
}

type LabResult struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Unit    string `json:"unit"`
	Norms   string `json:"norms"`
	InRange *bool  `json:"in_range"`
	ReadyAt string `json:"ready_at"`
}

type LabPanel struct {
	PatdirecID    int         `json:"patdirec_id"`
	PanelName     string      `json:"panel_name"`
	OrderedAt     string      `json:"ordered_at"`
	ReadyAt       string      `json:"ready_at"`
	TestsCount    int         `json:"tests_count"`
	HasOutOfRange bool        `json:"has_out_of_range"`
	Tests         []LabResult `json:"tests"`
}

// ===== Методы =====

func (c *Client) SearchByPhone(ctx context.Context, phone string) ([]Patient, error) {
	q := url.Values{}
	q.Set("phone", phone)

	var patients []Patient
	if err := c.get(ctx, "/api/patients/search?"+q.Encode(), &patients); err != nil {
		return nil, err
	}
	return patients, nil
}

func (c *Client) GetLabPanels(ctx context.Context, patientID, daysBack int) ([]LabPanel, error) {
	q := url.Values{}
	q.Set("patient_id", strconv.Itoa(patientID))
	if daysBack > 0 {
		q.Set("days_back", strconv.Itoa(daysBack))
	}

	var panels []LabPanel
	if err := c.get(ctx, "/api/patients/lab-panels?"+q.Encode(), &panels); err != nil {
		return nil, err
	}
	return panels, nil
}

func (c *Client) get(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return err
	}
	// api-gateway ожидает именно "Bearer <token>" (это наш собственный middleware)
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gateway %d: %s", resp.StatusCode, string(data))
	}

	var api apiResponse
	if err := json.Unmarshal(data, &api); err != nil {
		return fmt.Errorf("парсинг ответа gateway: %w", err)
	}
	if !api.Success {
		return fmt.Errorf("gateway error: %s", api.Error)
	}
	if out == nil || len(api.Data) == 0 {
		return nil
	}
	return json.Unmarshal(api.Data, out)
}
