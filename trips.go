package cripper

import (
	"github.com/umahmood/haversine"
)

func ClassifyTrip(in <-chan *Trip) <-chan *Trip {
	out := make(chan *Trip, 100)
	go func() {
		for trip := range in {
			if trip.EndChargeLevel > trip.StartChargeLevel {
				trip.Type = CHARGING_TRIP
				out <- trip
				continue
			}
			// Scooters usually don't loose more than a percent of energy during relocation
			if (trip.StartChargeLevel-trip.EndChargeLevel) < 1.1 && trip.Distance > 1.0 {
				trip.Type = RELOCATION_TRIP
				out <- trip
				continue
			}
			trip.Type = CUSTOMER_TRIP
			out <- trip
		}
		close(out)
	}()
	return out
}

type TripAggregator struct {
	unfinishedTrips map[string]*Trip
	lastScooters    Scooters
}

func NewTripAggregator() *TripAggregator {
	return &TripAggregator{
		unfinishedTrips: make(map[string]*Trip),
		lastScooters:    NewScooters([]*Scooter{}),
	}
}

func (t *TripAggregator) Aggregate(in <-chan ScrapeResult) <-chan *Trip {
	out := make(chan *Trip, 100)
	go func() {
		for res := range in {
			scooters := NewScooters(res.Scooters())
			vanishedScooter := scooters.Difference(t.lastScooters)
			for id, scooter := range vanishedScooter {
				trip := &Trip{
					ScooterID:        id,
					ScooterProvider:  "circ",
					StartChargeLevel: float64(scooter.ChargeLevel),
					StartLocation:    scooter.Location,
					StartTime:        res.ScrapeDate(),
				}
				t.unfinishedTrips[id] = trip
			}

			for id, trip := range t.unfinishedTrips {
				if scooter, exists := scooters[id]; exists {
					trip.EndChargeLevel = float64(scooter.ChargeLevel)
					trip.EndLocation = scooter.Location
					trip.UserID = scooter.StateUpdatedByUserID
					trip.EndTime = res.ScrapeDate()
					trip.Duration = trip.EndTime.Sub(trip.StartTime)
					trip.Cost = uint64(scooter.InitPrice + (scooter.UnitPrice * int(trip.Duration.Minutes())))

					_, distanceKm := haversine.Distance(
						haversine.Coord{Lat: trip.StartLocation.Latitude, Lon: trip.StartLocation.Longitude},
						haversine.Coord{Lat: trip.EndLocation.Latitude, Lon: trip.EndLocation.Longitude},
					)
					trip.Distance = distanceKm
					delete(t.unfinishedTrips, id)
					out <- trip
				}
			}
			t.lastScooters = scooters
		}
		close(out)
	}()
	return out
}

// Scooters is a map of Scooters in a ScrapeResult. This makes it easier to create differences
// from other sets of Scooters and to look up Scooters
type Scooters map[string]*Scooter

// NewScooters creates a new map based Scooters type from a slice of Scooters
func NewScooters(in []*Scooter) Scooters {
	s := make(Scooters)
	for _, scooter := range in {
		s[scooter.ID] = scooter
	}
	return s
}

// Difference returns all scooters which exist in ns but not in this Scooters instance
func (s Scooters) Difference(ns Scooters) Scooters {
	s2 := make(Scooters)

	for id, scooter := range ns {
		if _, exists := s[id]; !exists {
			s2[id] = scooter
		}
	}
	return s2
}
