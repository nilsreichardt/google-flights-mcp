// This example iterates over PriceGraph offers concurrently and prints only
// those whose price is cheaper than the low price of the offer.
// (The price is considered low by Google Flights)
// This example is the same as Example 2, but it sends requests concurrently.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/krisukox/google-flights-api/flights"
	"github.com/krisukox/google-flights-api/internal/cheapoffers"
	"golang.org/x/text/currency"
	"golang.org/x/text/language"
)

func main() {
	t := time.Now()

	session, err := flights.New()
	if err != nil {
		log.Fatal(err)
	}

	logger := log.New(os.Stdout, "", 0)

	options := flights.Options{
		Travelers: flights.Travelers{Adults: 1},
		Currency:  currency.USD,
		Stops:     flights.AnyStops,
		Class:     flights.Economy,
		TripType:  flights.RoundTrip,
		Lang:      language.English,
	}

	results, err := cheapoffers.Find(
		context.Background(),
		session,
		cheapoffers.Args{
			RangeStartDate: time.Now().AddDate(0, 0, 60),
			RangeEndDate:   time.Now().AddDate(0, 0, 90),
			TripLengths:    []int{7},
			SrcCities:      []string{"San Francisco", "San Jose"},
			DstCities:      []string{"New York", "Philadelphia", "Washington"},
			Options:        options,
		},
	)
	if err != nil {
		logger.Fatal(err)
	}

	for _, offer := range results {
		logger.Printf("%s %s\n", offer.StartDate, offer.ReturnDate)
		logger.Printf("trip length %d days\n", offer.TripLength)
		logger.Printf("price %d\n", int(offer.Price))
		logger.Println(offer.ShareableLink)
	}

	fmt.Println(time.Since(t))
}
