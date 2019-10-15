package main

import (
	"flag"
	"log"
	"time"

	"github.com/dereulenspiegel/sharealyzer"
	"github.com/dereulenspiegel/sharealyzer/circ"
	"github.com/umahmood/haversine"
)

var (
	timeFormat = "2006-01-02T15:04"
	baseDir    = flag.String("baseDir", "./out", "Base directory with scraped circ data")
	startTime  = flag.String("startTime", "2019-10-06T00:01", "Parseable time string with  a start time and date")
	endTime    = flag.String("endTime", "2019-10-07T00:01", "Parseable end time")
)

func main() {
	flag.Parse()
	aggregator := NewCircAggregator(*baseDir)

	start, err := time.Parse(timeFormat, *startTime)
	if err != nil {
		log.Fatalf("Failed to parse start time: %s", err)
	}
	end, err := time.Parse(timeFormat, *endTime)
	if err != nil {
		log.Fatalf("Failed to parse end time: %s", err)
	}
	log.Printf("Looking at a duration of %.2f hours", end.Sub(start).Hours())

	uniqueScooterIDs, err := aggregator.AggregateUniqueScooters(start, end)
	if err == nil {
		log.Printf("%d different scooters seem to be active", len(uniqueScooterIDs))
	} else {
		log.Printf("Finished with error: %s, %d different scooters seem to be active", err, len(uniqueScooterIDs))
	}

	uniqueUserIDs := make(map[string]bool)
	err = aggregator.Aggregate(start, end, func(fileDate time.Time, scooters []*circ.Scooter) error {
		for _, scooter := range scooters {
			if !uniqueUserIDs[scooter.StateUpdatedByUserIdentifier] {
				uniqueUserIDs[scooter.StateUpdatedByUserIdentifier] = true
			}
		}
		return nil
	})
	log.Printf("Have found %d unique userIDs", len(uniqueUserIDs))

	lastScooters := make(scooters)
	var trips []*sharealyzer.Trip
	var unusuallyLongTrips []*sharealyzer.Trip
	var chargingTrips []*sharealyzer.Trip
	unfinishedTrips := make(map[string]*sharealyzer.Trip)
	filesInspected := 0
	err = aggregator.Aggregate(start, end, func(fileTime time.Time, sc []*circ.Scooter) error {
		scooters := newScooters(sc)
		vanishedScooter := scooters.difference(lastScooters)

		for id, scooter := range vanishedScooter {
			//log.Printf("Starting trip for scooter: %s", scooter.Identifier)
			trip := &sharealyzer.Trip{
				ScooterID:        id,
				ScooterProvider:  "circ",
				StartChargeLevel: float64(scooter.EnergyLevel),
				StartLocation:    sharealyzer.NewGeoLocation(scooter.Latitude, scooter.Longitude),
				StartTime:        fileTime,
			}
			unfinishedTrips[id] = trip
		}
		for id, trip := range unfinishedTrips {
			if scooter, exists := scooters[id]; exists {
				//log.Printf("Ending trip for scooter: %s", scooter.Identifier)
				//Scooter is available again, trip is finished

				trip.EndChargeLevel = float64(scooter.EnergyLevel)
				trip.EndLocation = sharealyzer.NewGeoLocation(scooter.Latitude, scooter.Longitude)
				trip.UserID = scooter.StateUpdatedByUserIdentifier
				trip.EndTime = fileTime
				trip.Duration = trip.EndTime.Sub(trip.StartTime)
				trip.Cost = uint64(scooter.InitPrice + (scooter.Price * int(trip.Duration.Minutes())))

				_, distanceKm := haversine.Distance(
					haversine.Coord{Lat: trip.StartLocation.Latitude, Lon: trip.StartLocation.Longitude},
					haversine.Coord{Lat: trip.EndLocation.Latitude, Lon: trip.EndLocation.Longitude},
				)
				trip.Distance = distanceKm
				if trip.StartChargeLevel-trip.EndChargeLevel > 0 && trip.Duration.Minutes() < 60.0 {
					trips = append(trips, trip)
				} else if trip.StartChargeLevel-trip.EndChargeLevel < 0 {
					//log.Printf("scooter %s was charged", scooter.Identifier)
					chargingTrips = append(chargingTrips, trip)
				} else if trip.Duration.Minutes() >= 60.0 {
					unusuallyLongTrips = append(unusuallyLongTrips, trip)
				}

				delete(unfinishedTrips, id)
			}
		}
		filesInspected = filesInspected + 1
		lastScooters = scooters
		return nil
	})
	log.Printf("Found %d charging trips in %d files", len(chargingTrips), filesInspected)
	totalCost := uint64(0)
	var maxTripDuration time.Duration
	var maxDistance float64
	for _, t := range trips {
		totalCost = totalCost + t.Cost
		if t.Duration.Seconds() > maxTripDuration.Seconds() {
			maxTripDuration = t.Duration
		}
		if t.Distance > maxDistance {
			maxDistance = t.Distance
		}
	}
	averageCost := float64(trips[0].Cost)
	averageBatteryUsage := float64(trips[0].StartChargeLevel - trips[0].EndChargeLevel)
	averageDistance := trips[0].Distance

	for i := 1; i < len(trips); i++ {
		averageDistance = (averageDistance + trips[i].Distance) / 2
		averageCost = (averageCost + float64(trips[i].Cost)) / 2
		usedEnergy := float64(trips[i].StartChargeLevel - trips[i].EndChargeLevel)
		if usedEnergy < 0.0 {
			log.Printf("[WARNING] False trip, the scooter was charged!")
		}
		averageBatteryUsage = (averageBatteryUsage + usedEnergy) / 2
	}

	log.Printf("Found %d trips, with \ntotal cost of %.2f € (average %.2f €)\n average energy usage of %.2f\nmax duration %.2f\naverage distance %.2fkm\nmax distance %.2f",
		len(trips), float64(totalCost)/100.0, averageCost/100.0, averageBatteryUsage, maxTripDuration.Minutes(), averageDistance, maxDistance)

	log.Printf("Got %d trips over 60 Minutes", len(unusuallyLongTrips))

	for _, t := range unusuallyLongTrips {
		log.Printf("Long trip with scooter %s\nUsedEnergy: %.2f\nTrip duration %.2f\nDistance: %.2fkm", t.ScooterID, t.StartChargeLevel-t.EndChargeLevel, t.Duration.Minutes(), t.Distance)
	}
}
