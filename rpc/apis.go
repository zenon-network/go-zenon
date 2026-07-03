// Package rpc registers the go-zenon API namespaces onto the
// JSON-RPC server framework in rpc/server. Each namespace pairs a
// wire prefix with the constructor of the API receiver whose
// exported methods become remotely callable.
package rpc

import (
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/rpc/api/subscribe"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
	"github.com/zenon-network/go-zenon/zenon"
)

// getApi builds the rpc.API entries for a single module name. The
// "ledger" case registers the query-only ledger API, while
// "ledgerSubscribe" registers the subscription API under the same
// "ledger" namespace so its subscribe method is reached as
// ledger.subscribe. The "embedded" case fans out into one entry per
// embedded contract, and "stats" exposes node statistics. An unknown
// module yields no entries.
func getApi(z zenon.Zenon, p2p *p2p.Server, apiModule string) []rpc.API {
	switch apiModule {
	case "ledger":
		return []rpc.API{
			{
				Namespace: "ledger",
				Version:   "1.0",
				Service:   api.NewLedgerApi(z),
				Public:    true,
			},
		}
	case "ledgerSubscribe":
		return []rpc.API{
			{
				Namespace: "ledger",
				Version:   "1.0",
				Service:   subscribe.GetSubscribeApi(),
				Public:    true,
			},
		}
	case "embedded":
		return []rpc.API{
			{
				Namespace: "embedded.token",
				Version:   "1.0",
				Service:   embedded.NewTokenApi(z),
				Public:    true,
			},
			{
				Namespace: "embedded.sentinel",
				Version:   "1.0",
				Service:   embedded.NewSentinelApi(z),
				Public:    true,
			},
			{
				Namespace: "embedded.pillar",
				Version:   "1.0",
				Service:   embedded.NewPillarApi(z, false),
				Public:    true,
			},
			{
				Namespace: "embedded.plasma",
				Version:   "1.0",
				Service:   embedded.NewPlasmaApi(z),
				Public:    true,
			},
			{
				Namespace: "embedded.stake",
				Version:   "1.0",
				Service:   embedded.NewStakeApi(z),
				Public:    true,
			},
			{
				Namespace: "embedded.swap",
				Version:   "1.0",
				Service:   embedded.NewSwapApi(z),
				Public:    true,
			},
			{
				Namespace: "embedded.spork",
				Version:   "1.0",
				Service:   embedded.NewSporkApi(z),
				Public:    true,
			},
			{
				Namespace: "embedded.accelerator",
				Version:   "1.0",
				Service:   embedded.NewAcceleratorApi(z),
				Public:    true,
			},
			{
				Namespace: "embedded.htlc",
				Version:   "1.0",
				Service:   embedded.NewHtlcApi(z),
				Public:    true,
			},
			{
				Namespace: "embedded.bridge",
				Version:   "1.0",
				Service:   embedded.NewBridgeApi(z),
				Public:    true,
			},
			{
				Namespace: "embedded.liquidity",
				Version:   "1.0",
				Service:   embedded.NewLiquidityApi(z),
				Public:    true,
			},
		}
	case "stats":
		return []rpc.API{
			{
				Namespace: "stats",
				Version:   "1.0",
				Service:   api.NewStatsApi(z, p2p),
				Public:    true,
			},
		}
	default:
		return []rpc.API{}
	}
}

// GetApis returns the rpc.API entries for the named modules,
// concatenating the result of getApi for each in order.
func GetApis(z zenon.Zenon, p2p *p2p.Server, apiModule ...string) []rpc.API {
	var apis []rpc.API
	for _, m := range apiModule {
		apis = append(apis, getApi(z, p2p, m)...)
	}
	return apis
}

// GetPublicApis returns the full set of public API entries: the
// ledger query and subscription namespaces, every embedded contract
// namespace, and the stats namespace.
func GetPublicApis(z zenon.Zenon, p2p *p2p.Server) []rpc.API {
	return GetApis(z, p2p, "ledger", "ledgerSubscribe", "embedded", "stats")
}
