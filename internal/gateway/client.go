package gateway

import (
	"context"
	"encoding/json"
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
	Success      bool            `json:"success"`
	Data         json.RawMessage `json:"data"`
	Error        string          `json:"error"`
	ErrorDetails *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error_details"`
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
	Service          string `json:"service"`
	IsWaitlist       bool   `json:"is_waitlist"`
	MessengerNotify  *int   `json:"messenger_notify"`
	NoSMSNotify      bool   `json:"no_sms_notify"`
	ApptVersion      string `json:"appt_version"`
	BranchID         int    `json:"branch_id"`
	BranchCode       string `json:"branch_code"`
	DateConsultation string `json:"date_consultation"`
	Status           int    `json:"status"`
}

type NewBooking struct {
	DueReminder
	CreatedAt string `json:"created_at"`
}

type BookingSnapshot struct {
	NewBooking
	Cancelled bool `json:"cancelled"`
}

type BookingCursor struct {
	CreatedAt  string `json:"created_at"`
	PlanningID int64  `json:"planning_id"`
}

type NewBookingsPage struct {
	Items      []NewBooking    `json:"items"`
	NextCursor *BookingCursor  `json:"next_cursor"`
	HasMore    bool            `json:"has_more"`
}

// ===== Методы =====

func (c *Client) SearchByPhone(ctx context.Context, phone string) ([]Patient, error) {
	var patients []Patient
	if err := c.postJSON(ctx, "search", "/api/patients/search", map[string]string{"phone": phone}, &patients); err != nil {
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

func (c *Client) BookingByPlanningID(ctx context.Context, planningID int64) (BookingSnapshot, error) {
	var row BookingSnapshot
	if err := c.get(ctx, "/api/notifications/booking/"+strconv.FormatInt(planningID, 10), &row); err != nil {
		return BookingSnapshot{}, err
	}
	return row, nil
}

func (c *Client) NewBookings(ctx context.Context, patientIDs []int64, cursor *BookingCursor, limit int) (NewBookingsPage, error) {
	body := map[string]any{"patient_ids": patientIDs, "limit": limit}
	if cursor != nil {
		body["cursor"] = cursor
	}
	var page NewBookingsPage
	if err := c.postJSON(ctx, "new-bookings", "/api/notifications/new-bookings", body, &page); err != nil {
		return NewBookingsPage{}, err
	}
	return page, nil
}

func (c *Client) postJSON(ctx context.Context, op, path string, payload any, out interface{}) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(string(raw)))
	if err != nil {
		return wrapNet(op, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	return c.doJSON(op, req, out)
}

func (c *Client) get(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return wrapNet("get", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	return c.doJSON("get", req, out)
}

func (c *Client) doJSON(op string, req *http.Request, out interface{}) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return wrapNet(op, err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		code := ""
		var api apiResponse
		_ = json.Unmarshal(data, &api)
		if api.ErrorDetails != nil {
			code = api.ErrorDetails.Code
		}
		return &Error{Op: op, Status: resp.StatusCode, Code: code}
	}

	var api apiResponse
	if err := json.Unmarshal(data, &api); err != nil {
		return &Error{Op: op, Err: err}
	}
	if !api.Success {
		code := ""
		if api.ErrorDetails != nil {
			code = api.ErrorDetails.Code
		}
		return &Error{Op: op, Code: code}
	}
	if out == nil || len(api.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(api.Data, out); err != nil {
		return &Error{Op: op, Err: err}
	}
	return nil
}
