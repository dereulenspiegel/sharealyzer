package circ

import "time"

// CircError represents an error returned by the circ API. Unfortunately error handling
// is pretty inconsistent so this is only a best effort
type CircError struct {
	Timestamp time.Time `json:"timestamp"`
	Status    int       `json:"status"`
	Err       string    `json:"error"`
	Message   string    `json:"message"`
	Path      string    `json:"path"`
}

func (c CircError) Error() string {
	return "[CircError] " + c.Err + ": " + c.Message
}

// AuthResponse is the data received after successfull authentication. It contains the auth tokens and your profile
type AuthResponse struct {
	ID                        uint64  `json:"id"`
	Identifier                string  `json:"identifier"`
	FirstName                 *string `json:"firstName"`
	LastName                  *string `json:"lastName"`
	Email                     *string `json:"email"`
	EmailVerified             bool    `json:"emailVerified"`
	PhoneMobile               string  `json:"phoneMobile"`
	PhoneMobileVerified       bool    `json:"phoneMobileVerified"`
	Birthday                  *string `json:"birthday"`
	Language                  *string `json:"language"`
	PaymentProviderRegistered bool    `json:"paymentProviderRegistered"`
	Statistic                 []struct {
		Unit        string `json:"unit"`
		Value       string `json:"value"`
		Measurement string `json:"measurement"`
	} `json:"statistic"`
	Addresses    []interface{} `json:"addresses`
	AccessToken  string        `json:"accessToken"`
	RefreshToken string        `json:"refreshToken"`
}

// TokenRefreshResponse is the response when successfully refreshing tokens
type TokenRefreshResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	UserID       uint64 `json:"userId"`
	UserUUID     string `json:"userUuid"`
}

// Scooter represents one circ scooter within its API
type Scooter struct {
	Actions                        []string `json:"actions"`
	Broken                         bool     `json:"broken"`
	BrokenUpdateAt                 *uint64  `json:"brokenUpdateAt"`
	BrokenUpdatedByUserIdentifier  *string  `json:"brokenUpdatedByUserIdentifier"`
	BrokenUpdatedUserType          *string  `json:"brokenUpdatedUserType"`
	Connected                      bool     `json:"connected"`
	Currency                       string   `json:"currency"`
	Description                    string   `json:"description"`
	EnergyLevel                    int      `json:"energyLevel"`
	GpsRefreshRate                 int      `json:"gpsRefreshRate"`
	HornTimeInMs                   int      `json:"hornTimeInMs"`
	Identifier                     string   `json:"identifier"`
	Image                          *string  `json:"image"`
	InitPrice                      int      `json:"initPrice"`
	LastGnssUpdate                 uint64   `json:"lastGnssUpdate"`
	Latitude                       float64  `json:"latitude"`
	Locked                         bool     `json:"locked"`
	Longitude                      float64  `json:"longitude"`
	Missing                        bool     `json:"missing"`
	MissingUpdateAt                *uint64  `json:"missingUpdateAt"`
	MissingUpdatedByUserIdentifier *string  `json:"missingUpdatedByUserIdentifier"`
	MissingUpdatedUserType         *string  `json:"missingUpdatedUserType"`
	Name                           string   `json:"name"`
	Partner                        string   `json:"partner"`
	Price                          int      `json:"price"`
	PriceTime                      int      `json:"priceTime"`
	QrCode                         string   `json:"qrCode"`
	State                          string   `json:"state"`
	StateUpdateAt                  uint64   `json:"stateUpdateAt"`
	StateUpdatedByUserIdentifier   string   `json:"stateUpdatedByUserIdentifier"`
	StateUpdatedUserType           string   `json:"stateUpdatedUserType"`
	StatusRefreshRate              int      `json:"statusRefreshRate"`
	Timestamp                      string   `json:"timestamp"`
	Type                           string   `json:"type"`
	ZoneIdentifier                 string   `json:"zoneIdentifier"`
}
