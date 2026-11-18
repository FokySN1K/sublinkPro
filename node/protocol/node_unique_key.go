package protocol

import (
	"encoding/json"
	"net/url"
	"strings"

	"sublink/utils"
)

// NodeUniqueKeyField is the stable node identifier emitted next to every
// exported node. Clients that understand it can attribute telemetry to a node
// even after name processing rewrites every node name to the same value.
// Clients that do not understand it ignore it: Clash/Mihomo skip unknown proxy
// keys, Xray ignores unknown JSON fields, and URI parsers ignore unknown query
// parameters.
const NodeUniqueKeyField = "sublinkpro_unique_key"

// AppendNodeUniqueKey adds the stable node key to a rendered node link.
// It is applied after the node rename step: renaming re-encodes the link
// through the protocol codec, which drops unknown fields.
//
// SSR links are a single base64 payload without a query grammar and Surge
// lines are generated separately, so both keep working without the key.
// Unregistered schemes are passed through verbatim, mirroring RenameNodeLink:
// raw links must stay exactly as the user supplied them.
func AppendNodeUniqueKey(link, key string) string {
	link = strings.TrimSpace(link)
	key = strings.TrimSpace(key)
	if link == "" || key == "" {
		return link
	}
	if detectProtocol(link) == nil {
		return link
	}

	lower := strings.ToLower(link)
	switch {
	case strings.HasPrefix(lower, "vmess://"):
		return appendVMessNodeUniqueKey(link, key)
	case strings.HasPrefix(lower, "ssr://"):
		return link
	default:
		return appendURIQueryNodeUniqueKey(link, key)
	}
}

// appendVMessNodeUniqueKey stores the key inside the base64 JSON payload so
// vmess:// links stay valid for every standard client.
func appendVMessNodeUniqueKey(link, key string) string {
	payload := strings.TrimSpace(link[len("vmess://"):])
	decoded := utils.Base64Decode(payload)
	if decoded == "" || decoded == payload {
		return link
	}

	var document map[string]any
	if err := json.Unmarshal([]byte(decoded), &document); err != nil || document == nil {
		return link
	}
	document[NodeUniqueKeyField] = key

	encoded, err := json.Marshal(document)
	if err != nil {
		return link
	}
	return "vmess://" + utils.Base64Encode(string(encoded))
}

// appendURIQueryNodeUniqueKey appends or replaces the key query parameter while
// keeping the node link name (URL fragment) untouched.
func appendURIQueryNodeUniqueKey(link, key string) string {
	base, fragment := link, ""
	if index := strings.IndexByte(link, '#'); index >= 0 {
		base, fragment = link[:index], link[index:]
	}

	param := NodeUniqueKeyField + "=" + url.QueryEscape(key)
	queryIndex := strings.IndexByte(base, '?')
	if queryIndex < 0 {
		return base + "?" + param + fragment
	}

	prefix, rawQuery := base[:queryIndex], base[queryIndex+1:]
	parts := strings.Split(rawQuery, "&")
	filtered := make([]string, 0, len(parts)+1)
	for _, part := range parts {
		if part == "" {
			continue
		}
		name := part
		if equals := strings.IndexByte(part, '='); equals >= 0 {
			name = part[:equals]
		}
		if strings.EqualFold(name, NodeUniqueKeyField) {
			continue
		}
		filtered = append(filtered, part)
	}
	filtered = append(filtered, param)
	return prefix + "?" + strings.Join(filtered, "&") + fragment
}
