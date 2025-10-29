package cheapoffers

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/krisukox/google-flights-api/flights"
)

// Args describes the search window and constraints for finding cheap offers.
type Args struct {
	RangeStartDate time.Time
	RangeEndDate   time.Time
	TripLengths    []int
	SrcCities      []string
	DstCities      []string
	Options        flights.Options
}

// Result captures the cheapest qualifying offer for a specific start date.
type Result struct {
	StartDate     time.Time
	ReturnDate    time.Time
	SrcAirport    string
	DstAirport    string
	Price         float64
	TripLength    int
	ShareableLink string
}

// Find locates offers cheaper than Google's advertised low price within the given range.
// It mirrors the behaviour of examples/example3 but returns structured data instead of logging.
func Find(ctx context.Context, session *flights.Session, args Args) ([]Result, error) {
	if err := validateArgs(args); err != nil {
		return nil, err
	}

	var allResults []Result

	for _, tripLength := range args.TripLengths {
		partial, err := findForTripLength(ctx, session, args, tripLength)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, partial...)
	}

	sort.Slice(allResults, func(i, j int) bool {
		if allResults[i].Price == allResults[j].Price {
			if allResults[i].StartDate.Equal(allResults[j].StartDate) {
				if allResults[i].ReturnDate.Equal(allResults[j].ReturnDate) {
					return allResults[i].TripLength < allResults[j].TripLength
				}
				return allResults[i].ReturnDate.Before(allResults[j].ReturnDate)
			}
			return allResults[i].StartDate.Before(allResults[j].StartDate)
		}
		return allResults[i].Price < allResults[j].Price
	})

	return allResults, nil
}

func findForTripLength(ctx context.Context, session *flights.Session, args Args, tripLength int) ([]Result, error) {
	priceGraphOffers, err := session.GetPriceGraph(
		ctx,
		flights.PriceGraphArgs{
			RangeStartDate: args.RangeStartDate,
			RangeEndDate:   args.RangeEndDate,
			TripLength:     tripLength,
			SrcCities:      args.SrcCities,
			DstCities:      args.DstCities,
			Options:        args.Options,
		},
	)
	if err != nil {
		return nil, err
	}

	ctxWithCancel, cancel := context.WithCancel(ctx)
	defer cancel()

	type resultOrError struct {
		result Result
		err    error
	}

	resultsCh := make(chan resultOrError, len(priceGraphOffers))

	var wg sync.WaitGroup
	wg.Add(len(priceGraphOffers))

	for _, priceGraphOffer := range priceGraphOffers {
		offer := priceGraphOffer
		go func() {
			defer wg.Done()

			fullOffers, _, err := session.GetOffers(
				ctxWithCancel,
				flights.Args{
					Date:       offer.StartDate,
					ReturnDate: offer.ReturnDate,
					SrcCities:  args.SrcCities,
					DstCities:  args.DstCities,
					Options:    args.Options,
				},
			)
			if err != nil {
				cancel()
				resultsCh <- resultOrError{err: err}
				return
			}

			var bestOffer flights.FullOffer
			for _, fullOffer := range fullOffers {
				if fullOffer.Price == 0 {
					continue
				}
				if bestOffer.Price == 0 || fullOffer.Price < bestOffer.Price {
					bestOffer = fullOffer
				}
			}
			if bestOffer.Price == 0 {
				return
			}

			_, priceRange, err := session.GetOffers(
				ctxWithCancel,
				flights.Args{
					Date:        bestOffer.StartDate,
					ReturnDate:  bestOffer.ReturnDate,
					SrcAirports: []string{bestOffer.SrcAirportCode},
					DstAirports: []string{bestOffer.DstAirportCode},
					Options:     args.Options,
				},
			)
			if err != nil {
				cancel()
				resultsCh <- resultOrError{err: err}
				return
			}
			if priceRange == nil {
				return
			}

			if bestOffer.Price >= priceRange.Low {
				return
			}

			url, err := session.SerializeURL(
				ctxWithCancel,
				flights.Args{
					Date:        bestOffer.StartDate,
					ReturnDate:  bestOffer.ReturnDate,
					SrcAirports: []string{bestOffer.SrcAirportCode},
					DstAirports: []string{bestOffer.DstAirportCode},
					Options:     args.Options,
				},
			)
			if err != nil {
				cancel()
				resultsCh <- resultOrError{err: err}
				return
			}

			resultsCh <- resultOrError{
				result: Result{
					StartDate:     bestOffer.StartDate,
					ReturnDate:    bestOffer.ReturnDate,
					SrcAirport:    bestOffer.SrcAirportCode,
					DstAirport:    bestOffer.DstAirportCode,
					Price:         bestOffer.Price,
					TripLength:    tripLength,
					ShareableLink: url,
				},
			}
		}()
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var (
		results  []Result
		firstErr error
	)

	for item := range resultsCh {
		if item.err != nil {
			if firstErr == nil {
				firstErr = item.err
			}
			continue
		}
		results = append(results, item.result)
	}

	if firstErr != nil {
		return nil, firstErr
	}

	return results, nil
}

func validateArgs(args Args) error {
	if len(args.TripLengths) == 0 {
		return fmt.Errorf("at least one trip length is required")
	}
	for _, l := range args.TripLengths {
		if l <= 0 {
			return fmt.Errorf("trip lengths must be positive")
		}
	}
	if args.RangeEndDate.Before(args.RangeStartDate) {
		return fmt.Errorf("rangeEndDate must be on or after rangeStartDate")
	}
	if len(args.SrcCities) == 0 {
		return fmt.Errorf("at least one source city is required")
	}
	if len(args.DstCities) == 0 {
		return fmt.Errorf("at least one destination city is required")
	}
	return nil
}
