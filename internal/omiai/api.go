package omiai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

type friendRequestCreate struct {
	QuicdialID string `json:"quicdial_id"`
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

func (c *APIClient) ListFriends(ctx context.Context, token string) ([]Friend, error) {
	var friends []Friend
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/friends", token, nil, &friends); err != nil {
		return nil, err
	}
	return friends, nil
}

func (c *APIClient) ListPendingRequests(ctx context.Context, token string) ([]PendingFriendRequest, error) {
	var requests []PendingFriendRequest
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/friends/requests", token, nil, &requests); err != nil {
		return nil, err
	}
	return requests, nil
}

func (c *APIClient) SendFriendRequest(ctx context.Context, token, quicdialID string) (FriendRequestResponse, error) {
	var response FriendRequestResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/friends/request", token, friendRequestCreate{QuicdialID: quicdialID}, &response)
	return response, err
}

func (c *APIClient) AcceptFriendRequest(ctx context.Context, token, friendshipID string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/v1/friends/"+friendshipID+"/accept", token, map[string]any{}, nil)
}

func (c *APIClient) DeclineFriendRequest(ctx context.Context, token, friendshipID string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/v1/friends/"+friendshipID+"/decline", token, map[string]any{}, nil)
}

func (c *APIClient) RemoveFriend(ctx context.Context, token, quicdialID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/api/v1/friends/"+quicdialID, token, nil, nil)
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

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
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

func (c *APIClient) doJSON(ctx context.Context, method, path, token string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return decodeAPIError(resp)
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
