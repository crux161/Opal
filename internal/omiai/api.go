package omiai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type APIClient struct {
	baseURL string
	http    *http.Client
}

type tokenResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

type loginRequest struct {
	QuicdialID string `json:"quicdial_id"`
	Password   string `json:"password"`
}

func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *APIClient) Login(ctx context.Context, quicdialID, password, deviceID string) (Session, error) {
	req := loginRequest{
		QuicdialID: strings.TrimSpace(quicdialID),
		Password:   password,
	}

	var response tokenResponse
	if err := c.postJSON(ctx, "/api/v1/auth/login", req, &response); err != nil {
		return Session{}, err
	}

	return Session{
		Token:    response.Token,
		DeviceID: deviceID,
		User:     response.User,
	}, nil
}

func (c *APIClient) Signup(ctx context.Context, req SignupRequest, deviceID string) (Session, error) {
	if strings.TrimSpace(req.AvatarID) == "" {
		req.AvatarID = "kyu-kun"
	}

	var response tokenResponse
	if err := c.postJSON(ctx, "/api/v1/auth/signup", req, &response); err != nil {
		return Session{}, err
	}

	return Session{
		Token:    response.Token,
		DeviceID: deviceID,
		User:     response.User,
	}, nil
}

func (c *APIClient) postJSON(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return decodeAPIError(resp)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func decodeAPIError(resp *http.Response) error {
	var payload struct {
		Detail any `json:"detail"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("api request failed with status %d", resp.StatusCode)
	}
	if payload.Detail == nil {
		return fmt.Errorf("api request failed with status %d", resp.StatusCode)
	}
	return fmt.Errorf("%v", payload.Detail)
}
