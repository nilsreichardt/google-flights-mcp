// This example gets PriceGraph offers and prints only those whose
// price is cheaper than the low price of the offer
// (the price is considered as low by the Google Flights)
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/krisukox/google-flights-api/flights"
)

func main() {
	t := time.Now()

	session, err := flights.New()
	if err != nil {
		log.Fatal(err)
	}

	offers, _, err := session.GetOffers(
		context.Background(),
		flights.Args{
			Date:       time.Now().AddDate(0, 0, 60),
			ReturnDate: time.Now().AddDate(0, 0, 90),
			SrcCities:  []string{"Berkeley"},
			DstCities:  []string{"New York", "San Diego", "Los Angeles", "Denver", "Las Vegas", "Austin", "Dallas", "Seattle"},
			Options: flights.Options{
				Travelers: flights.Travelers{Adults: 1},
				Stops:     flights.AnyStops,
				Class:     flights.Economy,
				TripType:  flights.RoundTrip,
			},
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	for _, offer := range offers {
		fmt.Println(offer)
	}

	fmt.Println(time.Since(t))
}
