# Google Flights MCP Server

This project hosts an implementation of the [Model Context Protocol (MCP)](https://spec.modelcontextprotocol.io/)
server that can surface Google Flights data. The MCP implementation exposes three tools:

- `find_cheapest_offers` — finds itineraries that undercut Google's advertised low price (optionally capped by `maxPrice`).
- `get_price_graph` — returns the raw price graph entries for a date window and trip length.
- `get_offers` — fetches the detailed itineraries (including legs and price history) for specific departure/return dates.

While the repository still contains the `flights` Go package used to talk to Google Flights, the primary entry point
is now the MCP server in `cmd/mcp-server`. The older package APIs remain for compatibility but are no longer the
focus of this project.

## Highlights

- MCP-compliant server ready to plug into tooling that supports the protocol.
- Cheap-offer finder filters by Google's low price (historically cheap) or an explicit `maxPrice` hard ceiling.
- Direct access to Google Flights price graph data and full itinerary listings via dedicated tools.
- Returns shareable Google Flights URLs so the itinerary can be inspected or booked quickly.

## Requirements

- Go 1.23 or newer.
- Access to the Google Flights web experience. Some regions require passing a CAPTCHA to obtain the
  `GOOGLE_ABUSE_EXEMPTION` cookie; the bundled `flights` session helper reads it from your browser profile via
  [`kooky`](https://github.com/browserutils/kooky).
- Connectivity to `www.google.com`.

## Running the MCP server

```bash
go run ./cmd/mcp-server
```

Command-line flags:

- `--host` (default `0.0.0.0` or `HOST` env var) – interface to bind.
- `--port` (default `8080` or `PORT` env var) – port to listen on.

The server speaks the MCP Server-Sent Events transport. Point your MCP client at `http://<host>:<port>` and it will
discover all three tools automatically.

## Deploying

### Render.com

[![Deploy to Render](https://render.com/images/deploy-to-render-button.svg)](https://render.com/deploy?repo=https://github.com/nilsreichardt/google-flights-mcp)

### Google Cloud Run

Set your project ID in `deploy.sh` and run:

```bash
./deploy.sh
```

## Tool contracts

### `find_cheapest_offers`

Input parameters (all currency values are interpreted in the selected currency):

- `rangeStartDate` *(required)* – earliest departure date to consider (`YYYY-MM-DD`).
- `rangeEndDate` *(required)* – latest departure date to consider (`YYYY-MM-DD`).
- `tripLengths` *(required)* – array of positive integers describing trip durations in days.
- `srcCities` *(required)* – list of origin cities as accepted by Google Flights.
- `dstCities` *(required)* – list of destination cities as accepted by Google Flights.
- `language` *(optional)* – BCP 47 language tag, default `en`.
- `currency` *(optional)* – ISO 4217 currency code, default `USD`.
- `adults`, `children`, `infantInSeat`, `infantOnLap` *(optional)* – traveler counts, adults default to `1`.
- `stops` *(optional)* – maximum stops (`nonstop`, `stop1`, `stop2`, `any`), default `any`.
- `class` *(optional)* – cabin class (`economy`, `premium_economy`, `business`, `first`), default `economy`.
- `tripType` *(optional)* – `round_trip` or `one_way`, default `round_trip`.
- `maxPrice` *(optional)* – hard price ceiling. When omitted, the server only returns flights that undercut Google's
  advertised “low price” for the itinerary.

Example MCP `call_tool` request payload:

```json
{
  "name": "find_cheapest_offers",
  "arguments": {
    "rangeStartDate": "2024-11-01",
    "rangeEndDate": "2024-11-15",
    "tripLengths": [5, 7],
    "srcCities": ["San Francisco"],
    "dstCities": ["New York"],
    "currency": "USD",
    "maxPrice": 350
  }
}
```

This tool responds with a short textual summary (suitable for chat display) and structured JSON containing the
collection of matching offers. Each offer includes start and return dates, airport codes, price, trip length, currency,
and a shareable Google Flights link.

### `get_price_graph`

Input parameters (provide at least one origin and one destination using either cities or airports):

- `rangeStartDate` *(required)* – earliest departure date to consider (`YYYY-MM-DD`).
- `rangeEndDate` *(required)* – latest departure date to consider (`YYYY-MM-DD`).
- `tripLength` *(required)* – number of days between departure and return (must be positive).
- `srcCities` / `srcAirports` *(optional)* – origin cities or IATA airport codes. At least one of the two lists must contain a value.
- `dstCities` / `dstAirports` *(optional)* – destination cities or IATA airport codes. At least one of the two lists must contain a value.
- `language`, `currency`, `adults`, `children`, `infantInSeat`, `infantOnLap`, `stops`, `class`, `tripType` *(optional)* – interpreted the same way as in `find_cheapest_offers`.

Example request:

```json
{
  "name": "get_price_graph",
  "arguments": {
    "rangeStartDate": "2024-11-01",
    "rangeEndDate": "2024-11-30",
    "tripLength": 5,
    "srcCities": ["San Francisco"],
    "dstCities": ["New York"],
    "currency": "USD"
  }
}
```

The tool returns the price graph entries (start date, return date, price) together with the currency that was used for the query.

### `get_offers`

Input parameters (provide at least one origin and one destination using either cities or airports):

- `date` *(required)* – departure date (`YYYY-MM-DD`).
- `returnDate` *(required)* – return date (`YYYY-MM-DD`).
- `srcCities` / `srcAirports` *(optional)* – origin cities or IATA airport codes. At least one of the two lists must contain a value.
- `dstCities` / `dstAirports` *(optional)* – destination cities or IATA airport codes. At least one of the two lists must contain a value.
- `language`, `currency`, `adults`, `children`, `infantInSeat`, `infantOnLap`, `stops`, `class`, `tripType` *(optional)* – interpreted the same way as in `find_cheapest_offers`.

Example request:

```json
{
  "name": "get_offers",
  "arguments": {
    "date": "2024-11-10",
    "returnDate": "2024-11-15",
    "srcAirports": ["SFO"],
    "dstAirports": ["JFK"],
    "currency": "USD",
    "class": "business"
  }
}
```

The tool returns the list of matching itineraries (including flight segments, durations, and airports) plus the price range
Google advertises for the route when available.

## Development notes

- Formatting: run `gofmt`.
- Tests: `go test ./...` (requires network access to fetch module dependencies on first run).
- Logging: both the MCP server and cheap-offer searcher emit verbose logs to aid debugging (`[MCP]` and `[CheapOffers]`
  prefixes).

## License

Distributed under the terms of the `LICENSE` file.

## Fork

This project is a fork of the [Google Flights API](https://github.com/krisukox/google-flights-api). The main difference is that it implements the [Model Context Protocol](https://spec.modelcontextprotocol.io/) and the `MaxPrice` parameter.
