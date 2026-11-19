package models

import (
	"strings"
	"sync"
	"time"
)

// shareNodeIndexTTL bounds how long the node key index may lag behind node
// edits. Reports arrive every few minutes, so a one minute staleness window
// keeps the ingest path cheap without a stale profile for long.
const shareNodeIndexTTL = time.Minute

// ShareNodeInfo is the resolved display metadata of one stable node key.
type ShareNodeInfo struct {
	NodeID      int
	Key         string
	Name        string
	Group       string
	Country     string
	AirportID   int
	AirportName string
}

var shareNodeIndexMu sync.Mutex
var shareNodeIndexBuiltAt time.Time
var shareNodeIndexCache map[string]ShareNodeInfo

// ResolveShareNodeKeys resolves stable node keys (sublinkpro_unique_key) back
// to nodes and their airports. Unknown keys stay empty; callers keep the raw
// key as a fallback display name so no traffic is dropped.
func ResolveShareNodeKeys(keys []string) map[string]ShareNodeInfo {
	resolved := make(map[string]ShareNodeInfo, len(keys))
	if len(keys) == 0 {
		return resolved
	}

	shareNodeIndexMu.Lock()
	if shareNodeIndexCache == nil || time.Since(shareNodeIndexBuiltAt) > shareNodeIndexTTL {
		shareNodeIndexCache = buildShareNodeIndex()
		shareNodeIndexBuiltAt = time.Now()
	}
	index := shareNodeIndexCache
	shareNodeIndexMu.Unlock()

	for _, key := range keys {
		key = NormalizeShareNodeKey(key)
		if key == "" {
			continue
		}
		if info, ok := index[key]; ok {
			resolved[key] = info
		}
	}
	return resolved
}

func buildShareNodeIndex() map[string]ShareNodeInfo {
	nodes := nodeCache.GetAll()
	index := make(map[string]ShareNodeInfo, len(nodes))
	airportsByName := make(map[string]Airport)
	for _, airport := range airportCache.GetAll() {
		name := strings.ToLower(strings.TrimSpace(airport.Name))
		if name != "" {
			airportsByName[name] = airport
		}
	}

	for _, node := range nodes {
		key := node.SublinkNodeUniqueKey()
		if key == "" {
			continue
		}
		info := ShareNodeInfo{
			NodeID:  node.ID,
			Key:     key,
			Name:    node.EffectiveName(),
			Group:   node.Group,
			Country: node.LinkCountry,
		}
		if airport, ok := resolveNodeAirport(node, airportsByName); ok {
			info.AirportID = airport.ID
			info.AirportName = airport.Name
		}
		// Duplicate content hashes (same node imported from several airports)
		// keep the first entry; the key identifies the proxy, not the pool.
		if _, exists := index[key]; !exists {
			index[key] = info
		}
	}
	return index
}

func resolveNodeAirport(node Node, airportsByName map[string]Airport) (Airport, bool) {
	if node.SourceID > 0 {
		if airport, err := GetAirportByID(node.SourceID); err == nil {
			if strings.EqualFold(strings.TrimSpace(airport.Name), strings.TrimSpace(node.Source)) {
				return *airport, true
			}
		}
	}
	name := strings.ToLower(strings.TrimSpace(node.Source))
	if name == "" || name == "manual" {
		return Airport{}, false
	}
	if airport, ok := airportsByName[name]; ok {
		return airport, true
	}
	return Airport{}, false
}
