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

type findCheapestOffersParams struct {
	RangeStartDate string   `json:"rangeStartDate" jsonschema:"Earliest departure date to consider (YYYY-MM-DD)"`
	RangeEndDate   string   `json:"rangeEndDate" jsonschema:"Last departure date to consider (YYYY-MM-DD)"`
	TripLengths    []int    `json:"tripLengths" jsonschema:"Trip lengths in days (e.g. [5,6])"`
	SrcCities      []string `json:"srcCities" jsonschema:"City names accepted by Google Flights"`
	DstCities      []string `json:"dstCities" jsonschema:"Destination city names accepted by Google Flights"`
	Language       string   `json:"language,omitempty" jsonschema:"Optional BCP 47 language tag, defaults to en"`
	Currency       string   `json:"currency,omitempty" jsonschema:"Optional ISO 4217 currency code, defaults to USD"`
	Adults         int      `json:"adults,omitempty" jsonschema:"Optional number of adult travelers, defaults to 1"`
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

type server struct {
	session *flights.Session
}

func (s *server) findCheapestOffers(ctx context.Context, _ *mcp.CallToolRequest, params findCheapestOffersParams) (*mcp.CallToolResult, findCheapestOffersResponse, error) {
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

	lang := language.English
	if params.Language != "" {
		var parseErr error
		lang, parseErr = language.Parse(params.Language)
		if parseErr != nil {
			return nil, findCheapestOffersResponse{}, fmt.Errorf("parse language: %w", parseErr)
		}
	}

	curr := currency.USD
	if params.Currency != "" {
		var parseErr error
		curr, parseErr = currency.ParseISO(params.Currency)
		if parseErr != nil {
			return nil, findCheapestOffersResponse{}, fmt.Errorf("parse currency: %w", parseErr)
		}
	}

	adults := params.Adults
	if adults == 0 {
		adults = 1
	}
	if adults < 0 {
		return nil, findCheapestOffersResponse{}, fmt.Errorf("adults must be greater than zero")
	}

	options := flights.Options{
		Travelers: flights.Travelers{Adults: adults},
		Currency:  curr,
		Stops:     flights.AnyStops,
		Class:     flights.Economy,
		TripType:  flights.RoundTrip,
		Lang:      lang,
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
			Name:        "Find Cheapest Offers",
			Title:       "Find cheapest Google Flights offers",
			Description: "Finds itineraries whose price is below Google's low price for the selected window.",
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
