package catalogue

import "marshrut/route"

type Snapshot struct {
	Schema       int                    `json:"schema"`
	Revision     uint64                 `json:"revision"`
	Rules        []route.Rule           `json:"rules"`
	Entries      []route.CatalogueEntry `json:"entries"`
	Applications []route.Application    `json:"applications"`
}

const TransferFormat = "marshrut-routing-catalogue"

// Transfer includes saved definitions and overrides. Automatically observed
// running processes and profile credentials belong to the local installation.
type Transfer struct {
	Format  string                 `json:"format"`
	Schema  int                    `json:"schema"`
	Entries []route.CatalogueEntry `json:"entries"`
	Rules   []route.Rule           `json:"rules"`
}
