package sharealyzer

import (
	"encoding/json"
	"time"
)

// ScooterState represents the current state the scooter is in. For most APIs this is probably IDLE_RENTABLE
type ScooterState string

// Constants for valid ScooterStates
const (
	IdleRentable ScooterState = "IDLE_RENTABLE"
	Broken       ScooterState = "BROKEN"
	InUse        ScooterState = "IN_USE"
)

// GeoLocation represents simple latitude and longitude based geographic locations
type GeoLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// NewGeoLocation is a helper to create GeoLocations from floats, representing latitude and longitude.
func NewGeoLocation(lat, long float64) *GeoLocation {
	return &GeoLocation{
		Latitude:  lat,
		Longitude: long,
	}
}

type ScrapeResult interface {
	ScrapeFile
	Scooters() []*Scooter
}

type DefaultScrapeResult struct {
	date     time.Time
	scooters []*Scooter
	provider string
}

func (d *DefaultScrapeResult) ScrapeDate() time.Time {
	return d.date
}

func (d *DefaultScrapeResult) Scooters() []*Scooter {
	return d.scooters
}

func (d *DefaultScrapeResult) Provider() string {
	return d.provider
}

func (d *DefaultScrapeResult) Content() []byte {
	data, _ := json.Marshal(d.scooters)
	return data
}

func NewScrapeResult(provider string, date time.Time, scooters []*Scooter) ScrapeResult {
	return &DefaultScrapeResult{
		date:     date,
		scooters: scooters,
		provider: provider,
	}
}

// Scooter represents a generic eScooter
type Scooter struct {
	ID                   string
	Provider             string
	State                ScooterState
	Location             *GeoLocation
	ChargeLevel          float64
	LastUpdate           time.Time
	QRContent            string
	StateUpdatedByUserID string
	InitPrice            int
	UnitPrice            int
}

type TripType string

const (
	CUSTOMER_TRIP   TripType = "CUSTOMER_TRIP"
	CHARGING_TRIP   TripType = "CHARGING_TRIP"
	RELOCATION_TRIP TripType = "RELOCATION_TRIP"
)

// Trip represents a user initiated journey between two locations.
type Trip struct {
	ID               string        `json:"id"`
	ScooterID        string        `json:"scooter_id"`
	ScooterProvider  string        `json:"provider"`
	StartChargeLevel float64       `json:"start_charge_level"`
	EndChargeLevel   float64       `json:"end_charge_level"`
	StartLocation    *GeoLocation  `json:"start_location"`
	EndLocation      *GeoLocation  `json:"end_location"`
	UserID           string        `json:"user_id"`
	Duration         time.Duration `json:"duration"`
	Cost             uint64        `json:"cost"` // Cost of the trip in euro cents
	StartTime        time.Time     `json:"start_time"`
	EndTime          time.Time     `json:"end_time"`
	Distance         float64       `json:"distance"` // Distance in kilometers
	Type             TripType
}

type TripStore interface {
	Store(t *Trip) error
}
