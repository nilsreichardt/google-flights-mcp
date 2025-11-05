package cheapoffers

import (
	"context"
	"fmt"
	"log"
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

// UnderPriceArgs captures the search window when filtering by an absolute price ceiling.
type UnderPriceArgs struct {
	RangeStartDate time.Time
	RangeEndDate   time.Time
	TripLengths    []int
	SrcCities      []string
	DstCities      []string
	Options        flights.Options
	MaxPrice       float64
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

// FindHistorical locates offers cheaper than Google's advertised low price within the given range.
// It mirrors the behaviour of examples/example3 but returns structured data instead of logging.
func FindHistorical(ctx context.Context, session *flights.Session, args Args) ([]Result, error) {
	log.Printf("[CheapOffers] FindHistorical called with:")
	log.Printf("[CheapOffers]   RangeStartDate: %s", args.RangeStartDate.Format("2006-01-02"))
	log.Printf("[CheapOffers]   RangeEndDate: %s", args.RangeEndDate.Format("2006-01-02"))
	log.Printf("[CheapOffers]   TripLengths: %v", args.TripLengths)
	log.Printf("[CheapOffers]   SrcCities: %v", args.SrcCities)
	log.Printf("[CheapOffers]   DstCities: %v", args.DstCities)
	log.Printf("[CheapOffers]   Options: %+v", args.Options)
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

	sortResults(allResults)

	log.Printf("[CheapOffers] FindHistorical completed: total %d cheap offers found", len(allResults))
	return allResults, nil
}

// Find is kept for backward compatibility. It delegates to FindHistorical.
func Find(ctx context.Context, session *flights.Session, args Args) ([]Result, error) {
	return FindHistorical(ctx, session, args)
}

func findForTripLength(ctx context.Context, session *flights.Session, args Args, tripLength int) ([]Result, error) {
	log.Printf("[CheapOffers] Processing trip length: %d days", tripLength)
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
	log.Printf("[CheapOffers] GetPriceGraph returned %d offers for trip length %d", len(priceGraphOffers), tripLength)
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

			log.Printf("[CheapOffers] Processing offer: %s -> %s, Price: %.0f",
				offer.StartDate.Format("2006-01-02"), offer.ReturnDate.Format("2006-01-02"),
				offer.Price)
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
			log.Printf("[CheapOffers] GetOffers returned %d full offers", len(fullOffers))
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
			if priceRange != nil {
				log.Printf("[CheapOffers] Price range for %s->%s: Low=%.0f, High=%.0f",
					bestOffer.SrcAirportCode, bestOffer.DstAirportCode,
					priceRange.Low, priceRange.High)
			}
			if err != nil {
				cancel()
				resultsCh <- resultOrError{err: err}
				return
			}
			if priceRange == nil {
				return
			}

			if bestOffer.Price >= priceRange.Low {
				log.Printf("[CheapOffers] Offer price %.0f >= low price %.0f, skipping", bestOffer.Price, priceRange.Low)
				return
			}
			log.Printf("[CheapOffers] Found cheap offer: %s->%s, price %.0f < low price %.0f",
				bestOffer.SrcAirportCode, bestOffer.DstAirportCode, bestOffer.Price, priceRange.Low)

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

	log.Printf("[CheapOffers] Completed processing trip length %d: found %d cheap offers", tripLength, len(results))

	return results, nil
}

// FindUnderPrice locates the cheapest offers for each candidate date whose price does not exceed args.MaxPrice.
func FindUnderPrice(ctx context.Context, session *flights.Session, args UnderPriceArgs) ([]Result, error) {
	log.Printf("[CheapOffers] FindUnderPrice called with:")
	log.Printf("[CheapOffers]   RangeStartDate: %s", args.RangeStartDate.Format("2006-01-02"))
	log.Printf("[CheapOffers]   RangeEndDate: %s", args.RangeEndDate.Format("2006-01-02"))
	log.Printf("[CheapOffers]   TripLengths: %v", args.TripLengths)
	log.Printf("[CheapOffers]   SrcCities: %v", args.SrcCities)
	log.Printf("[CheapOffers]   DstCities: %v", args.DstCities)
	log.Printf("[CheapOffers]   Options: %+v", args.Options)
	log.Printf("[CheapOffers]   MaxPrice: %.0f", args.MaxPrice)

	if err := validateUnderPriceArgs(args); err != nil {
		return nil, err
	}

	type searchRequest struct {
		startDate  time.Time
		returnDate time.Time
		tripLength int
	}

	var requests []searchRequest
	for start := args.RangeStartDate; !start.After(args.RangeEndDate); start = start.AddDate(0, 0, 1) {
		for _, tripLength := range args.TripLengths {
			requests = append(requests, searchRequest{
				startDate:  start,
				returnDate: start.AddDate(0, 0, tripLength),
				tripLength: tripLength,
			})
		}
	}

	if len(requests) == 0 {
		return nil, nil
	}

	ctxWithCancel, cancel := context.WithCancel(ctx)
	defer cancel()

	type resultOrError struct {
		result Result
		err    error
	}

	resultsCh := make(chan resultOrError, len(requests))

	var wg sync.WaitGroup
	wg.Add(len(requests))

	for _, req := range requests {
		request := req
		go func() {
			defer wg.Done()

			log.Printf("[CheapOffers] Processing start %s (trip length %d)",
				request.startDate.Format("2006-01-02"), request.tripLength)
			fullOffers, _, err := session.GetOffers(
				ctxWithCancel,
				flights.Args{
					Date:       request.startDate,
					ReturnDate: request.returnDate,
					SrcCities:  args.SrcCities,
					DstCities:  args.DstCities,
					Options:    args.Options,
				},
			)
			log.Printf("[CheapOffers] GetOffers returned %d offers", len(fullOffers))
			if err != nil {
				cancel()
				resultsCh <- resultOrError{err: err}
				return
			}

			var bestOffer flights.FullOffer
			for _, offer := range fullOffers {
				if offer.Price == 0 || offer.Price > args.MaxPrice {
					continue
				}
				if bestOffer.Price == 0 || offer.Price < bestOffer.Price {
					bestOffer = offer
				}
			}
			if bestOffer.Price == 0 {
				return
			}

			log.Printf("[CheapOffers] Found offer under max price: %s->%s, price %.0f <= max %.0f",
				bestOffer.SrcAirportCode, bestOffer.DstAirportCode, bestOffer.Price, args.MaxPrice)

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
					TripLength:    request.tripLength,
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
		allResults []Result
		firstErr   error
	)
	for item := range resultsCh {
		if item.err != nil {
			if firstErr == nil {
				firstErr = item.err
			}
			continue
		}
		allResults = append(allResults, item.result)
	}
	if firstErr != nil {
		return nil, firstErr
	}

	sortResults(allResults)

	log.Printf("[CheapOffers] FindUnderPrice completed: total %d offers under %.0f found", len(allResults), args.MaxPrice)
	return allResults, nil
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

func validateUnderPriceArgs(args UnderPriceArgs) error {
	base := Args{
		RangeStartDate: args.RangeStartDate,
		RangeEndDate:   args.RangeEndDate,
		TripLengths:    args.TripLengths,
		SrcCities:      args.SrcCities,
		DstCities:      args.DstCities,
		Options:        args.Options,
	}
	if err := validateArgs(base); err != nil {
		return err
	}
	if args.MaxPrice <= 0 {
		return fmt.Errorf("maxPrice must be greater than zero")
	}
	return nil
}

func sortResults(results []Result) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Price == results[j].Price {
			if results[i].StartDate.Equal(results[j].StartDate) {
				if results[i].ReturnDate.Equal(results[j].ReturnDate) {
					return results[i].TripLength < results[j].TripLength
				}
				return results[i].ReturnDate.Before(results[j].ReturnDate)
			}
			return results[i].StartDate.Before(results[j].StartDate)
		}
		return results[i].Price < results[j].Price
	})
}
