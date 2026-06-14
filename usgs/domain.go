// Package usgs exposes the USGS Earthquake catalog as a kit Domain driver.
//
// A multi-domain host (ant) enables it with a single blank import:
//
//	import _ "github.com/tamnd/usgs-cli/usgs"
//
// The same Domain also builds the standalone usgs binary (see cli.NewApp).
package usgs

import (
	"context"
	"time"

	"github.com/tamnd/any-cli/kit"
)

func init() { kit.Register(Domain{}) }

// Domain is the usgs driver.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against,
// and the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "usgs",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "usgs",
			Short:  "USGS global earthquake catalog",
			Long: `usgs queries the USGS FDSNWS earthquake catalog for seismic events worldwide.

No API key required. Data from the USGS Comprehensive Earthquake Catalog (ComCat).`,
			Site: Host,
			Repo: "https://github.com/tamnd/usgs-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{
		Name:    "query",
		Group:   "read",
		List:    true,
		Summary: "Search earthquakes by magnitude, date, and order",
	}, queryOp)

	kit.Handle(app, kit.OpMeta{
		Name:    "recent",
		Group:   "read",
		List:    true,
		Summary: "Significant earthquakes in the last 30 days (mag >= 6)",
	}, recentOp)
}

// newClient builds the USGS client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := DefaultConfig()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.Timeout = cfg.Timeout
	}
	return NewClient(c), nil
}

// --- inputs ---

type queryInput struct {
	Start   string  `kit:"flag" help:"start date (YYYY-MM-DD); defaults to last 24h"`
	End     string  `kit:"flag" help:"end date (YYYY-MM-DD)"`
	MinMag  string  `kit:"flag" help:"minimum magnitude" default:"3.0"`
	MaxMag  string  `kit:"flag" help:"maximum magnitude"`
	Limit   int     `kit:"flag,inherit" help:"max results (1-20000)" default:"20"`
	OrderBy string  `kit:"flag" help:"sort order: time, time-asc, magnitude, magnitude-asc" default:"time"`
	Client  *Client `kit:"inject"`
}

type recentInput struct {
	Client *Client `kit:"inject"`
}

// --- handlers ---

func queryOp(ctx context.Context, in queryInput, emit func(Earthquake) error) error {
	start := in.Start
	if start == "" {
		start = time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	eqs, err := in.Client.Query(ctx, QueryParams{
		Start:   start,
		End:     in.End,
		MinMag:  in.MinMag,
		MaxMag:  in.MaxMag,
		Limit:   limit,
		OrderBy: in.OrderBy,
	})
	if err != nil {
		return err
	}
	for _, eq := range eqs {
		if err := emit(eq); err != nil {
			return err
		}
	}
	return nil
}

func recentOp(ctx context.Context, in recentInput, emit func(Earthquake) error) error {
	eqs, err := in.Client.Recent(ctx)
	if err != nil {
		return err
	}
	for _, eq := range eqs {
		if err := emit(eq); err != nil {
			return err
		}
	}
	return nil
}

// Classify turns any input into the canonical (type, id).
func (Domain) Classify(input string) (string, string, error) {
	return "event", input, nil
}

// Locate returns the live USGS URL for a (type, id).
func (Domain) Locate(t, id string) (string, error) {
	return "https://earthquake.usgs.gov/earthquakes/eventpage/" + id, nil
}
