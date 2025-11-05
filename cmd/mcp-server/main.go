package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/krisukox/google-flights-api/flights"
	"github.com/krisukox/google-flights-api/internal/cheapoffers"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/text/currency"
	"golang.org/x/text/language"
)

var (
	hostDefault = envString("HOST", "0.0.0.0")
	portDefault = envInt("PORT", 8080)
	host        = flag.String("host", hostDefault, "host interface to listen on")
	port        = flag.Int("port", portDefault, "port to listen on")
)

type travelOptionsParams struct {
	Language     string `json:"language,omitempty" jsonschema:"Optional BCP 47 language tag, defaults to en"`
	Currency     string `json:"currency,omitempty" jsonschema:"Optional ISO 4217 currency code, defaults to USD"`
	Adults       int    `json:"adults,omitempty" jsonschema:"Optional number of adult travelers, defaults to 1"`
	Children     int    `json:"children,omitempty" jsonschema:"Optional number of child travelers, defaults to 0"`
	InfantInSeat int    `json:"infantInSeat,omitempty" jsonschema:"Optional number of infants in seat, defaults to 0"`
	InfantOnLap  int    `json:"infantOnLap,omitempty" jsonschema:"Optional number of lap infants, defaults to 0"`
	Stops        string `json:"stops,omitempty" jsonschema:"Optional maximum stops preference: nonstop, stop1, stop2, any (defaults to any)"`
	Class        string `json:"class,omitempty" jsonschema:"Optional cabin class: economy, premium_economy, business, first (defaults to economy)"`
	TripType     string `json:"tripType,omitempty" jsonschema:"Optional trip type: round_trip or one_way (defaults to round_trip)"`
}

type findCheapestOffersParams struct {
	RangeStartDate string   `json:"rangeStartDate" jsonschema:"Earliest departure date to consider (YYYY-MM-DD)"`
	RangeEndDate   string   `json:"rangeEndDate" jsonschema:"Last departure date to consider (YYYY-MM-DD)"`
	TripLengths    []int    `json:"tripLengths" jsonschema:"Trip lengths in days (e.g. [5,6])"`
	SrcCities      []string `json:"srcCities" jsonschema:"City names accepted by Google Flights"`
	DstCities      []string `json:"dstCities" jsonschema:"Destination city names accepted by Google Flights"`
	travelOptionsParams
	MaxPrice *float64 `json:"maxPrice,omitempty" jsonschema:"Optional maximum price threshold in the selected currency. If not provided, historical low prices are used."`
}

type offerResponse struct {
	StartDate     string  `json:"startDate"`
	ReturnDate    string  `json:"returnDate"`
	SrcAirport    string  `json:"srcAirport"`
	DstAirport    string  `json:"dstAirport"`
	Price         float64 `json:"price"`
	TripLength    int     `json:"tripLength"`
	Currency      string  `json:"currency"`
	ShareableLink string  `json:"shareableLink"`
}

type findCheapestOffersResponse struct {
	Offers []offerResponse `json:"offers"`
}

type getPriceGraphParams struct {
	RangeStartDate string   `json:"rangeStartDate" jsonschema:"Earliest departure date to consider (YYYY-MM-DD)"`
	RangeEndDate   string   `json:"rangeEndDate" jsonschema:"Last departure date to consider (YYYY-MM-DD)"`
	TripLength     int      `json:"tripLength" jsonschema:"Number of days between departure and return date"`
	SrcCities      []string `json:"srcCities,omitempty" jsonschema:"Optional list of origin cities accepted by Google Flights"`
	SrcAirports    []string `json:"srcAirports,omitempty" jsonschema:"Optional list of origin airport IATA codes"`
	DstCities      []string `json:"dstCities,omitempty" jsonschema:"Optional list of destination cities accepted by Google Flights"`
	DstAirports    []string `json:"dstAirports,omitempty" jsonschema:"Optional list of destination airport IATA codes"`
	travelOptionsParams
}

type priceGraphOfferResponse struct {
	StartDate  string  `json:"startDate"`
	ReturnDate string  `json:"returnDate"`
	Price      float64 `json:"price"`
}

type getPriceGraphResponse struct {
	Offers   []priceGraphOfferResponse `json:"offers"`
	Currency string                    `json:"currency"`
}

type getOffersParams struct {
	Date        string   `json:"date" jsonschema:"Departure date to search (YYYY-MM-DD)"`
	ReturnDate  string   `json:"returnDate" jsonschema:"Return date to search (YYYY-MM-DD)"`
	SrcCities   []string `json:"srcCities,omitempty" jsonschema:"Optional list of origin cities accepted by Google Flights"`
	SrcAirports []string `json:"srcAirports,omitempty" jsonschema:"Optional list of origin airport IATA codes"`
	DstCities   []string `json:"dstCities,omitempty" jsonschema:"Optional list of destination cities accepted by Google Flights"`
	DstAirports []string `json:"dstAirports,omitempty" jsonschema:"Optional list of destination airport IATA codes"`
	travelOptionsParams
}

type flightSegmentResponse struct {
	DepAirportCode string `json:"depAirportCode"`
	DepAirportName string `json:"depAirportName"`
	DepCity        string `json:"depCity"`
	ArrAirportName string `json:"arrAirportName"`
	ArrAirportCode string `json:"arrAirportCode"`
	ArrCity        string `json:"arrCity"`
	DepTime        string `json:"depTime"`
	ArrTime        string `json:"arrTime"`
	Duration       string `json:"duration"`
	Airplane       string `json:"airplane"`
	FlightNumber   string `json:"flightNumber"`
	AirlineName    string `json:"airlineName"`
	Legroom        string `json:"legroom"`
}

type fullOfferResponse struct {
	StartDate            string                  `json:"startDate"`
	ReturnDate           string                  `json:"returnDate"`
	Price                float64                 `json:"price"`
	SrcAirport           string                  `json:"srcAirport"`
	DstAirport           string                  `json:"dstAirport"`
	SrcCity              string                  `json:"srcCity"`
	DstCity              string                  `json:"dstCity"`
	FlightDuration       string                  `json:"flightDuration"`
	ReturnFlightDuration string                  `json:"returnFlightDuration,omitempty"`
	Flight               []flightSegmentResponse `json:"flight"`
	ReturnFlight         []flightSegmentResponse `json:"returnFlight,omitempty"`
}

type priceRangeResponse struct {
	Low  float64 `json:"low"`
	High float64 `json:"high"`
}

type getOffersResponse struct {
	Offers     []fullOfferResponse `json:"offers"`
	PriceRange *priceRangeResponse `json:"priceRange,omitempty"`
	Currency   string              `json:"currency"`
}

var enumValueReplacer = strings.NewReplacer("-", "", "_", "", " ", "")

func normalizeEnumValue(val string) string {
	return enumValueReplacer.Replace(strings.ToLower(val))
}

func parseStops(val string) (flights.Stops, error) {
	switch normalizeEnumValue(val) {
	case "nonstop":
		return flights.Nonstop, nil
	case "stop1", "onestop", "1stop":
		return flights.Stop1, nil
	case "stop2", "twostop", "2stop":
		return flights.Stop2, nil
	case "any", "anystops":
		return flights.AnyStops, nil
	default:
		return 0, fmt.Errorf("invalid stops %q: valid values are nonstop, stop1, stop2, any", val)
	}
}

func parseClass(val string) (flights.Class, error) {
	switch normalizeEnumValue(val) {
	case "economy":
		return flights.Economy, nil
	case "premiumeconomy":
		return flights.PremiumEconomy, nil
	case "business":
		return flights.Business, nil
	case "first":
		return flights.First, nil
	default:
		return 0, fmt.Errorf("invalid class %q: valid values are economy, premium_economy, business, first", val)
	}
}

func parseTripType(val string) (flights.TripType, error) {
	switch normalizeEnumValue(val) {
	case "roundtrip", "round":
		return flights.RoundTrip, nil
	case "oneway":
		return flights.OneWay, nil
	default:
		return 0, fmt.Errorf("invalid tripType %q: valid values are round_trip, one_way", val)
	}
}

func logTravelOptions(params travelOptionsParams) {
	log.Printf("[MCP]   Language: %s", params.Language)
	log.Printf("[MCP]   Currency: %s", params.Currency)
	log.Printf("[MCP]   Adults: %d", params.Adults)
	log.Printf("[MCP]   Children: %d", params.Children)
	log.Printf("[MCP]   InfantInSeat: %d", params.InfantInSeat)
	log.Printf("[MCP]   InfantOnLap: %d", params.InfantOnLap)
	log.Printf("[MCP]   Stops: %s", params.Stops)
	log.Printf("[MCP]   Class: %s", params.Class)
	log.Printf("[MCP]   TripType: %s", params.TripType)
}

func buildFlightOptions(params travelOptionsParams) (flights.Options, currency.Unit, error) {
	lang := language.English
	if params.Language != "" {
		var parseErr error
		lang, parseErr = language.Parse(params.Language)
		if parseErr != nil {
			return flights.Options{}, currency.Unit{}, fmt.Errorf("parse language: %w", parseErr)
		}
	}

	curr := currency.USD
	if params.Currency != "" {
		var parseErr error
		curr, parseErr = currency.ParseISO(params.Currency)
		if parseErr != nil {
			return flights.Options{}, currency.Unit{}, fmt.Errorf("parse currency: %w", parseErr)
		}
	}

	adults := params.Adults
	if adults == 0 {
		adults = 1
	}
	if adults < 0 {
		return flights.Options{}, currency.Unit{}, fmt.Errorf("adults must be greater than zero")
	}
	if params.Children < 0 {
		return flights.Options{}, currency.Unit{}, fmt.Errorf("children must be zero or greater")
	}
	if params.InfantInSeat < 0 {
		return flights.Options{}, currency.Unit{}, fmt.Errorf("infantInSeat must be zero or greater")
	}
	if params.InfantOnLap < 0 {
		return flights.Options{}, currency.Unit{}, fmt.Errorf("infantOnLap must be zero or greater")
	}

	stops := flights.AnyStops
	if params.Stops != "" {
		var parseErr error
		stops, parseErr = parseStops(params.Stops)
		if parseErr != nil {
			return flights.Options{}, currency.Unit{}, parseErr
		}
	}

	class := flights.Economy
	if params.Class != "" {
		var parseErr error
		class, parseErr = parseClass(params.Class)
		if parseErr != nil {
			return flights.Options{}, currency.Unit{}, parseErr
		}
	}

	tripType := flights.RoundTrip
	if params.TripType != "" {
		var parseErr error
		tripType, parseErr = parseTripType(params.TripType)
		if parseErr != nil {
			return flights.Options{}, currency.Unit{}, parseErr
		}
	}

	travelers := flights.Travelers{
		Adults:       adults,
		Children:     params.Children,
		InfantInSeat: params.InfantInSeat,
		InfantOnLap:  params.InfantOnLap,
	}

	options := flights.Options{
		Travelers: travelers,
		Currency:  curr,
		Stops:     stops,
		Class:     class,
		TripType:  tripType,
		Lang:      lang,
	}

	return options, curr, nil
}

func convertFlightSegments(segments []flights.Flight) []flightSegmentResponse {
	if len(segments) == 0 {
		return nil
	}

	out := make([]flightSegmentResponse, 0, len(segments))
	for _, segment := range segments {
		out = append(out, flightSegmentResponse{
			DepAirportCode: segment.DepAirportCode,
			DepAirportName: segment.DepAirportName,
			DepCity:        segment.DepCity,
			ArrAirportName: segment.ArrAirportName,
			ArrAirportCode: segment.ArrAirportCode,
			ArrCity:        segment.ArrCity,
			DepTime:        segment.DepTime.Format(time.RFC3339),
			ArrTime:        segment.ArrTime.Format(time.RFC3339),
			Duration:       segment.Duration.String(),
			Airplane:       segment.Airplane,
			FlightNumber:   segment.FlightNumber,
			AirlineName:    segment.AirlineName,
			Legroom:        segment.Legroom,
		})
	}
	return out
}

type server struct {
	session *flights.Session
}

func (s *server) findCheapestOffers(ctx context.Context, _ *mcp.CallToolRequest, params findCheapestOffersParams) (*mcp.CallToolResult, findCheapestOffersResponse, error) {
	log.Printf("[MCP] findCheapestOffers called with parameters:")
	log.Printf("[MCP]   RangeStartDate: %s", params.RangeStartDate)
	log.Printf("[MCP]   RangeEndDate: %s", params.RangeEndDate)
	log.Printf("[MCP]   TripLengths: %v", params.TripLengths)
	log.Printf("[MCP]   SrcCities: %v", params.SrcCities)
	log.Printf("[MCP]   DstCities: %v", params.DstCities)
	logTravelOptions(params.travelOptionsParams)
	if params.MaxPrice != nil {
		log.Printf("[MCP]   MaxPrice: %.0f", *params.MaxPrice)
	} else {
		log.Printf("[MCP]   MaxPrice: <nil>")
	}
	startDate, err := time.Parse(time.DateOnly, params.RangeStartDate)
	if err != nil {
		return nil, findCheapestOffersResponse{}, fmt.Errorf("parse rangeStartDate: %w", err)
	}
	endDate, err := time.Parse(time.DateOnly, params.RangeEndDate)
	if err != nil {
		return nil, findCheapestOffersResponse{}, fmt.Errorf("parse rangeEndDate: %w", err)
	}
	if len(params.TripLengths) == 0 {
		return nil, findCheapestOffersResponse{}, fmt.Errorf("tripLengths must contain at least one value")
	}
	for _, l := range params.TripLengths {
		if l <= 0 {
			return nil, findCheapestOffersResponse{}, fmt.Errorf("tripLengths must be positive values")
		}
	}
	if len(params.SrcCities) == 0 {
		return nil, findCheapestOffersResponse{}, fmt.Errorf("at least one source city is required")
	}
	if len(params.DstCities) == 0 {
		return nil, findCheapestOffersResponse{}, fmt.Errorf("at least one destination city is required")
	}
	if params.MaxPrice != nil && *params.MaxPrice <= 0 {
		return nil, findCheapestOffersResponse{}, fmt.Errorf("maxPrice must be greater than zero")
	}

	options, curr, err := buildFlightOptions(params.travelOptionsParams)
	if err != nil {
		return nil, findCheapestOffersResponse{}, err
	}

	results, err := cheapoffers.Find(
		ctx,
		s.session,
		cheapoffers.Args{
			RangeStartDate: startDate,
			RangeEndDate:   endDate,
			TripLengths:    params.TripLengths,
			SrcCities:      params.SrcCities,
			DstCities:      params.DstCities,
			Options:        options,
			MaxPrice:       params.MaxPrice,
		},
	)
	if err != nil {
		return nil, findCheapestOffersResponse{}, err
	}

	response := findCheapestOffersResponse{Offers: make([]offerResponse, 0, len(results))}
	for _, res := range results {
		response.Offers = append(response.Offers, offerResponse{
			StartDate:     res.StartDate.Format(time.RFC3339),
			ReturnDate:    res.ReturnDate.Format(time.RFC3339),
			SrcAirport:    res.SrcAirport,
			DstAirport:    res.DstAirport,
			Price:         res.Price,
			TripLength:    res.TripLength,
			Currency:      curr.String(),
			ShareableLink: res.ShareableLink,
		})
	}

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("Found %d cheap offer(s).", len(response.Offers)))
	if len(response.Offers) > 0 {
		cheapest := response.Offers[0]
		summary.WriteString(fmt.Sprintf(" Cheapest: %s -> %s on %s for %.0f %s (%d days).",
			cheapest.SrcAirport,
			cheapest.DstAirport,
			cheapest.StartDate,
			cheapest.Price,
			cheapest.Currency,
			cheapest.TripLength,
		))
	}

	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: summary.String()},
		},
	}
	log.Printf("[MCP] Returning %d offers to client", len(response.Offers))
	return result, response, nil
}

func (s *server) getPriceGraph(ctx context.Context, _ *mcp.CallToolRequest, params getPriceGraphParams) (*mcp.CallToolResult, getPriceGraphResponse, error) {
	log.Printf("[MCP] getPriceGraph called with parameters:")
	log.Printf("[MCP]   RangeStartDate: %s", params.RangeStartDate)
	log.Printf("[MCP]   RangeEndDate: %s", params.RangeEndDate)
	log.Printf("[MCP]   TripLength: %d", params.TripLength)
	log.Printf("[MCP]   SrcCities: %v", params.SrcCities)
	log.Printf("[MCP]   SrcAirports: %v", params.SrcAirports)
	log.Printf("[MCP]   DstCities: %v", params.DstCities)
	log.Printf("[MCP]   DstAirports: %v", params.DstAirports)
	logTravelOptions(params.travelOptionsParams)

	startDate, err := time.Parse(time.DateOnly, params.RangeStartDate)
	if err != nil {
		return nil, getPriceGraphResponse{}, fmt.Errorf("parse rangeStartDate: %w", err)
	}
	endDate, err := time.Parse(time.DateOnly, params.RangeEndDate)
	if err != nil {
		return nil, getPriceGraphResponse{}, fmt.Errorf("parse rangeEndDate: %w", err)
	}
	if params.TripLength <= 0 {
		return nil, getPriceGraphResponse{}, fmt.Errorf("tripLength must be greater than zero")
	}

	options, curr, err := buildFlightOptions(params.travelOptionsParams)
	if err != nil {
		return nil, getPriceGraphResponse{}, err
	}

	args := flights.PriceGraphArgs{
		RangeStartDate: startDate,
		RangeEndDate:   endDate,
		TripLength:     params.TripLength,
		SrcCities:      params.SrcCities,
		SrcAirports:    params.SrcAirports,
		DstCities:      params.DstCities,
		DstAirports:    params.DstAirports,
		Options:        options,
	}
	if err := args.Validate(); err != nil {
		return nil, getPriceGraphResponse{}, err
	}

	offers, err := s.session.GetPriceGraph(ctx, args)
	if err != nil {
		return nil, getPriceGraphResponse{}, err
	}

	response := getPriceGraphResponse{
		Offers:   make([]priceGraphOfferResponse, 0, len(offers)),
		Currency: curr.String(),
	}

	var (
		cheapestOffer flights.Offer
		hasCheapest   bool
	)
	for i := range offers {
		offer := offers[i]
		response.Offers = append(response.Offers, priceGraphOfferResponse{
			StartDate:  offer.StartDate.Format(time.RFC3339),
			ReturnDate: offer.ReturnDate.Format(time.RFC3339),
			Price:      offer.Price,
		})
		if !hasCheapest || offer.Price < cheapestOffer.Price {
			cheapestOffer = offer
			hasCheapest = true
		}
	}

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("Found %d price graph offer(s).", len(response.Offers)))
	if hasCheapest {
		summary.WriteString(fmt.Sprintf(" Lowest price: depart %s return %s for %.0f %s.",
			cheapestOffer.StartDate.Format(time.DateOnly),
			cheapestOffer.ReturnDate.Format(time.DateOnly),
			cheapestOffer.Price,
			curr.String(),
		))
	}

	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: summary.String()},
		},
	}
	log.Printf("[MCP] Returning %d price graph offers to client", len(response.Offers))
	return result, response, nil
}

func (s *server) getOffers(ctx context.Context, _ *mcp.CallToolRequest, params getOffersParams) (*mcp.CallToolResult, getOffersResponse, error) {
	log.Printf("[MCP] getOffers called with parameters:")
	log.Printf("[MCP]   Date: %s", params.Date)
	log.Printf("[MCP]   ReturnDate: %s", params.ReturnDate)
	log.Printf("[MCP]   SrcCities: %v", params.SrcCities)
	log.Printf("[MCP]   SrcAirports: %v", params.SrcAirports)
	log.Printf("[MCP]   DstCities: %v", params.DstCities)
	log.Printf("[MCP]   DstAirports: %v", params.DstAirports)
	logTravelOptions(params.travelOptionsParams)

	date, err := time.Parse(time.DateOnly, params.Date)
	if err != nil {
		return nil, getOffersResponse{}, fmt.Errorf("parse date: %w", err)
	}
	returnDate, err := time.Parse(time.DateOnly, params.ReturnDate)
	if err != nil {
		return nil, getOffersResponse{}, fmt.Errorf("parse returnDate: %w", err)
	}

	options, curr, err := buildFlightOptions(params.travelOptionsParams)
	if err != nil {
		return nil, getOffersResponse{}, err
	}

	args := flights.Args{
		Date:        date,
		ReturnDate:  returnDate,
		SrcCities:   params.SrcCities,
		SrcAirports: params.SrcAirports,
		DstCities:   params.DstCities,
		DstAirports: params.DstAirports,
		Options:     options,
	}
	if err := args.ValidateOffersArgs(); err != nil {
		return nil, getOffersResponse{}, err
	}

	offers, priceRange, err := s.session.GetOffers(ctx, args)
	if err != nil {
		return nil, getOffersResponse{}, err
	}

	response := getOffersResponse{
		Offers:   make([]fullOfferResponse, 0, len(offers)),
		Currency: curr.String(),
	}
	if priceRange != nil {
		response.PriceRange = &priceRangeResponse{
			Low:  priceRange.Low,
			High: priceRange.High,
		}
	}

	var (
		cheapestOffer flights.FullOffer
		hasCheapest   bool
	)
	for i := range offers {
		offer := offers[i]
		flightSegments := convertFlightSegments(offer.Flight)
		returnFlightSegments := convertFlightSegments(offer.ReturnFlight)

		returnFlightDuration := ""
		if offer.ReturnFlightDuration > 0 {
			returnFlightDuration = offer.ReturnFlightDuration.String()
		}

		response.Offers = append(response.Offers, fullOfferResponse{
			StartDate:            offer.StartDate.Format(time.RFC3339),
			ReturnDate:           offer.ReturnDate.Format(time.RFC3339),
			Price:                offer.Price,
			SrcAirport:           offer.SrcAirportCode,
			DstAirport:           offer.DstAirportCode,
			SrcCity:              offer.SrcCity,
			DstCity:              offer.DstCity,
			FlightDuration:       offer.FlightDuration.String(),
			ReturnFlightDuration: returnFlightDuration,
			Flight:               flightSegments,
			ReturnFlight:         returnFlightSegments,
		})

		if !hasCheapest || (offer.Price > 0 && offer.Price < cheapestOffer.Price) {
			cheapestOffer = offer
			hasCheapest = true
		}
	}

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("Found %d offer(s).", len(response.Offers)))
	if hasCheapest {
		summary.WriteString(fmt.Sprintf(" Lowest price: %s -> %s departing %s for %.0f %s.",
			cheapestOffer.SrcAirportCode,
			cheapestOffer.DstAirportCode,
			cheapestOffer.StartDate.Format(time.DateOnly),
			cheapestOffer.Price,
			curr.String(),
		))
	}

	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: summary.String()},
		},
	}
	log.Printf("[MCP] Returning %d offers to client (GetOffers)", len(response.Offers))
	return result, response, nil
}

func main() {
	flag.Parse()

	session, err := flights.New()
	if err != nil {
		log.Fatalf("create session: %v", err)
	}

	s := &server{session: session}

	impl := &mcp.Implementation{
		Name:    "google_flights_cheapest_offers",
		Version: "0.1.0",
	}

	mcpServer := mcp.NewServer(impl, nil)
	mcp.AddTool(
		mcpServer,
		&mcp.Tool{
			Name:        "get_price_graph",
			Title:       "Get Google Flights price graph",
			Description: "Retrieves Google Flights price graph offers for the specified window and trip length.",
		},
		s.getPriceGraph,
	)
	mcp.AddTool(
		mcpServer,
		&mcp.Tool{
			Name:        "get_offers",
			Title:       "Get Google Flights offers",
			Description: "Fetches detailed Google Flights itineraries (including price range) for specific dates.",
		},
		s.getOffers,
	)
	mcp.AddTool(
		mcpServer,
		&mcp.Tool{
			Name:        "find_cheapest_offers",
			Title:       "Find cheapest Google Flights offers",
			Description: "Finds itineraries priced below Google's low price (or an optional max price) for the selected window.",
		},
		s.findCheapestOffers,
	)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	handler := mcp.NewSSEHandler(func(r *http.Request) *mcp.Server {
		return mcpServer
	}, nil)

	log.Printf("MCP server listening on %s (SSE)", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Printf("HTTP server error: %v", err)
		os.Exit(1)
	}
}

func envString(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func envInt(name string, fallback int) int {
	if v := os.Getenv(name); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return fallback
}
