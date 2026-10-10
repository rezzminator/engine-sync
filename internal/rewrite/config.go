package rewrite

import "github.com/rezzminator/engine-sync/internal/config"

func Configured(rows []config.Rewrite) Chain {
	chain := make(Chain, 0, len(rows))
	for _, r := range rows {
		chain = append(chain, Literal{Engines: r.Engines, Kinds: r.Kinds, Text: r.Literal, Replace: r.Replace})
	}
	return chain
}
