package circ

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

const (
	loginURL        = `https://node.goflash.com/verification/phone/start`
	signupURL       = `https://node.goflash.com/signup/phone`
	tokenRefreshURL = `https://node.goflash.com/login/refresh`
	devicesURL      = `https://node.goflash.com/devices`
)

var (
	// DefaultTokenRefreshDuration is the duration we usually wait before refreshing our tokens
	DefaultTokenRefreshDuration = time.Minute * 5
)

// TokenStore is a simple interface to store and retrieve the currently used tokens
type TokenStore interface {
	Store(accessToken, refreshToken string) error
	Load() (accessToken string, refreshToken string, er error)
}

// WithTokenStore sets the specified TokenStore as token store of the circ client
func WithTokenStore(store TokenStore) ClientOption {
	return func(c *CircClient) {
		c.tokenStore = store
	}
}

// WithHTTPClient allows you to specify a custom http client instead of Go's default client
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *CircClient) {
		c.httpClient = client
	}
}

// ClientOption lets you specify options for the client
type ClientOption func(c *CircClient)

// CircClient is a client to the circ API
type CircClient struct {
	httpClient *http.Client

	accessToken      string
	refreshToken     string
	lastTokenRefresh time.Time
	tokenStore       TokenStore
}

// New creates a new client for the Circ API with the specified options
func New(opts ...ClientOption) *CircClient {
	c := &CircClient{
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}

	if c.tokenStore != nil {
		accesstoken, refreshtoken, err := c.tokenStore.Load()
		if err == nil {
			c.accessToken = accesstoken
			c.refreshToken = refreshtoken
		}
	}
	return c
}

func (c *CircClient) checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 400 {
		fmt.Printf("Received error from circ API")
		var circErr CircError
		if err := json.NewDecoder(resp.Body).Decode(&circErr); err != nil {
			circErr.Status = resp.StatusCode
			circErr.Message = err.Error()
		}
		return circErr
	}
	return nil
}

func (c *CircClient) request(method string, url string, body io.Reader) (*http.Request, error) {
	r, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-type", "application/json")
	r.Header.Set("Accept", "application/json")

	if c.accessToken != "" {
		r.Header.Set("Authorization", c.accessToken)
	}

	return r, nil
}

func (c *CircClient) refreshAuth() error {
	if time.Now().Before(c.lastTokenRefresh.Add(DefaultTokenRefreshDuration)) {
		return nil
	}
	defer func() {
		c.lastTokenRefresh = time.Now()
	}()
	buf := &bytes.Buffer{}
	json.NewEncoder(buf).Encode(map[string]string{
		"accessToken":  c.accessToken,
		"refreshToken": c.refreshToken,
	})
	r, err := c.request(http.MethodPost, tokenRefreshURL, buf)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	var refreshResponse TokenRefreshResponse
	if err := json.Unmarshal(body, &refreshResponse); err != nil {
		circErr := CircError{
			Status:  resp.StatusCode,
			Err:     err.Error(),
			Message: "Unknown error",
		}
		return circErr
	}
	c.accessToken = refreshResponse.AccessToken
	c.refreshToken = refreshResponse.RefreshToken
	if c.tokenStore != nil {
		if err = c.tokenStore.Store(c.accessToken, c.refreshToken); err != nil {
			return nil
		}
	}
	return nil
}

// ForceTokenRefresh forces a token refresh, used for testing
func (c *CircClient) ForceTokenRefresh() error {
	return c.refreshAuth()
}

// Login starts authentication against the circ API you need to specify your phone numbers country prefiy
// i.e. '+49' and your phone number without the leading zero and a callback function which returns the received
// auth token.
func (c *CircClient) Login(countryCode, phoneNumber string, provideCode func() string) error {
	buf := &bytes.Buffer{}
	json.NewEncoder(buf).Encode(map[string]string{
		"phoneCountryCode": countryCode,
		"phoneNumber":      phoneNumber,
	})

	r, err := c.request(http.MethodPost, loginURL, buf)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := c.checkResponse(resp); err != nil {
		return err
	}
	authCode := provideCode()

	buf.Reset()
	json.NewEncoder(buf).Encode(map[string]string{
		"phoneCountryCode": countryCode,
		"phoneNumber":      phoneNumber,
		"token":            authCode,
	})

	r, err = c.request(http.MethodPost, signupURL, buf)
	if err != nil {
		return err
	}
	resp, err = c.httpClient.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := c.checkResponse(resp); err != nil {
		return err
	}
	var authResponse AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResponse); err != nil {
		return err
	}

	c.accessToken = authResponse.AccessToken
	c.refreshToken = authResponse.RefreshToken
	if c.tokenStore != nil {
		if err := c.tokenStore.Store(c.accessToken, c.refreshToken); err != nil {
			return err
		}
	}
	return nil
}

// Scooters returns all available scooters at this point in time. You need to specify the area
// to scrape as a rectangle with a top left and a bottom right corner. It is unknown how large
// this rectangle can get before things break down
func (c *CircClient) Scooters(latitudeTopLeft,
	longitudeTopLeft, latitudeBottomRight, longitudeBottomRight float64) ([]*Scooter, error) {

	if err := c.refreshAuth(); err != nil {
		return nil, err
	}
	r, err := c.request(http.MethodGet, devicesURL, nil)
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	q.Add("latitudeTopLeft", floatToString(latitudeTopLeft))
	q.Add("longitudeTopLeft", floatToString(longitudeTopLeft))
	q.Add("latitudeBottomRight", floatToString(latitudeBottomRight))
	q.Add("longitudeBottomRight", floatToString(longitudeBottomRight))
	r.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := c.checkResponse(resp); err != nil {
		return nil, err
	}
	body, _ := ioutil.ReadAll(resp.Body)
	devicesResponse := struct {
		Devices []*Scooter `json:"devices"`
		Total   int        `json:"total"`
	}{}
	if err := json.Unmarshal(body, &devicesResponse); err != nil {
		log.Printf("Unexpected body (code: %d): %s", resp.StatusCode, string(body))
		return nil, err
	}
	return devicesResponse.Devices, nil
}

func floatToString(in float64) string {
	return fmt.Sprintf("%.5f", in)
}
