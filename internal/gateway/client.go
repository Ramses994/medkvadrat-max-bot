package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

type DueReminder struct {
	PlanningID       int64  `json:"planning_id"`
	PatientID        int64  `json:"patient_id"`
	PatientPhone     string `json:"patient_phone"`
	PatientName      string `json:"patient_name"`
	DoctorName       string `json:"doctor_name"`
	DepartmentID     int    `json:"department_id"`
	DepartmentLabel  string `json:"department_label"`
	DateConsultation string `json:"date_consultation"`
	Status           int    `json:"status"`
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

func (c *Client) DueReminders(ctx context.Context, from, to time.Time, patientIDs []int64) ([]DueReminder, error) {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("MSK", 3*3600)
	}
	const layout = "2006-01-02T15:04:05"
	q := url.Values{}
	q.Set("from", from.In(loc).Format(layout))
	q.Set("to", to.In(loc).Format(layout))
	if len(patientIDs) > 0 {
		parts := make([]string, len(patientIDs))
		for i, id := range patientIDs {
			parts[i] = strconv.FormatInt(id, 10)
		}
		q.Set("patient_ids", strings.Join(parts, ","))
	}

	var rows []DueReminder
	if err := c.get(ctx, "/api/reminders/due?"+q.Encode(), &rows); err != nil {
		return nil, err
	}
	return rows, nil
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
