package common

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/tidwall/gjson"
)

var relayModeNames = map[int]string{
	relayconstant.RelayModeChatCompletions:    "chat_completions",
	relayconstant.RelayModeCompletions:        "completions",
	relayconstant.RelayModeEmbeddings:         "embeddings",
	relayconstant.RelayModeModerations:        "moderations",
	relayconstant.RelayModeImagesGenerations:  "images_generations",
	relayconstant.RelayModeImagesEdits:        "images_edits",
	relayconstant.RelayModeEdits:              "edits",
	relayconstant.RelayModeAudioSpeech:        "audio_speech",
	relayconstant.RelayModeAudioTranscription: "audio_transcription",
	relayconstant.RelayModeAudioTranslation:   "audio_translation",
	relayconstant.RelayModeRerank:             "rerank",
	relayconstant.RelayModeResponses:          "responses",
	relayconstant.RelayModeResponsesCompact:   "responses_compact",
	relayconstant.RelayModeRealtime:           "realtime",
	relayconstant.RelayModeGemini:             "gemini",
}

func normalizeRelayModeName(mode string) string {
	replacer := strings.NewReplacer("-", "_", " ", "_")
	return strings.ToLower(strings.TrimSpace(replacer.Replace(mode)))
}

func relayModeName(mode int) string {
	return relayModeNames[mode]
}

func relayModeMatches(expected string, actual int) bool {
	expected = normalizeRelayModeName(expected)
	if expected == "" {
		return true
	}
	if numeric, err := strconv.Atoi(expected); err == nil {
		return numeric == actual
	}
	return expected == relayModeName(actual)
}

func parseRequestPathAndQuery(raw string) (string, map[string]string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "/", map[string]string{}
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		if parsed, err := url.Parse(trimmed); err == nil {
			return parsed.EscapedPath(), valuesToStringMap(parsed.Query())
		}
	}
	if parsed, err := url.ParseRequestURI(trimmed); err == nil {
		path := parsed.EscapedPath()
		if path == "" {
			path = "/"
		}
		return path, valuesToStringMap(parsed.Query())
	}
	return trimmed, map[string]string{}
}

func valuesToStringMap(values url.Values) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		result[key] = values.Get(key)
	}
	return result
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func buildRouteTemplateContext(info *RelayInfo) map[string]string {
	ctx := map[string]string{
		"model":          strings.TrimSpace(info.UpstreamModelName),
		"original_model": strings.TrimSpace(info.OriginModelName),
		"request_path":   strings.TrimSpace(GetResolvedRequestPath(info, info.RequestURLPath)),
		"request_method": strings.TrimSpace(GetResolvedRequestMethod(info, info.RequestMethod)),
	}
	if ctx["model"] == "" {
		ctx["model"] = ctx["original_model"]
	}
	return ctx
}

func applyRouteTemplate(raw string, info *RelayInfo) string {
	templated := raw
	for key, value := range buildRouteTemplateContext(info) {
		templated = strings.ReplaceAll(templated, "{"+key+"}", value)
	}
	return templated
}

func GetResolvedRequestMethod(info *RelayInfo, fallback string) string {
	if info == nil {
		return fallback
	}
	method := strings.TrimSpace(info.ResolvedRequestMethod)
	if method == "" {
		method = strings.TrimSpace(fallback)
	}
	if method == "" {
		method = "POST"
	}
	return strings.ToUpper(method)
}

func GetResolvedRequestPath(info *RelayInfo, fallback string) string {
	if info == nil {
		path, _ := parseRequestPathAndQuery(fallback)
		return path
	}
	if strings.TrimSpace(info.ResolvedRequestPath) != "" {
		return info.ResolvedRequestPath
	}
	path, _ := parseRequestPathAndQuery(fallback)
	return path
}

func GetResolvedRequestQuery(info *RelayInfo) map[string]string {
	if info == nil {
		return map[string]string{}
	}
	if len(info.ResolvedRequestQuery) > 0 {
		return cloneStringMap(info.ResolvedRequestQuery)
	}
	_, query := parseRequestPathAndQuery(info.RequestURLPath)
	return query
}

func BuildResolvedRequestPathWithQuery(info *RelayInfo, fallback string) string {
	path := GetResolvedRequestPath(info, fallback)
	query := GetResolvedRequestQuery(info)
	if len(query) == 0 {
		return path
	}
	values := url.Values{}
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		values.Set(key, query[key])
	}
	return path + "?" + values.Encode()
}

func ResolveChannelRestRoute(info *RelayInfo) {
	if info == nil || info.ChannelMeta == nil {
		return
	}
	routes := info.ChannelOtherSettings.RestRoutes
	if len(routes) == 0 {
		return
	}

	requestPath, requestQuery := parseRequestPathAndQuery(info.RequestURLPath)
	requestMethod := strings.ToUpper(strings.TrimSpace(info.RequestMethod))
	if requestMethod == "" {
		requestMethod = "POST"
	}

	modelName := strings.TrimSpace(info.UpstreamModelName)
	if modelName == "" {
		modelName = strings.TrimSpace(info.OriginModelName)
	}

	sortedRoutes := make([]dto.RestRouteRule, 0, len(routes))
	for _, route := range routes {
		sortedRoutes = append(sortedRoutes, route)
	}
	sort.SliceStable(sortedRoutes, func(i, j int) bool {
		return sortedRoutes[i].Priority > sortedRoutes[j].Priority
	})

	for _, route := range sortedRoutes {
		if !route.Enabled {
			continue
		}
		match := route.Match
		if match.RequestPath != "" && strings.TrimSpace(match.RequestPath) != requestPath {
			continue
		}
		if match.RequestPathPrefix != "" && !strings.HasPrefix(requestPath, strings.TrimSpace(match.RequestPathPrefix)) {
			continue
		}
		if match.Method != "" && strings.ToUpper(strings.TrimSpace(match.Method)) != requestMethod {
			continue
		}
		if match.RelayMode != "" && !relayModeMatches(match.RelayMode, info.RelayMode) {
			continue
		}
		if match.ModelPrefix != "" && !strings.HasPrefix(modelName, strings.TrimSpace(match.ModelPrefix)) {
			continue
		}
		// check request body field conditions
		if !checkBodyFieldCondition(info.RequestBodyJson, match.RequestBodyFieldExists, true) {
			continue
		}
		if !checkBodyFieldCondition(info.RequestBodyJson, match.RequestBodyFieldMissing, false) {
			continue
		}

		targetPathRaw := strings.TrimSpace(route.Target.Path)
		if targetPathRaw == "" {
			targetPathRaw = requestPath
		}
		targetPathRaw = applyRouteTemplate(targetPathRaw, info)
		targetPath, targetQuery := parseRequestPathAndQuery(targetPathRaw)

		mergedQuery := cloneStringMap(requestQuery)
		for key, value := range targetQuery {
			mergedQuery[key] = value
		}
		for key, value := range route.Target.Query {
			mergedQuery[strings.TrimSpace(key)] = applyRouteTemplate(value, info)
		}

		info.ResolvedRequestMethod = requestMethod
		if strings.TrimSpace(route.Target.Method) != "" {
			info.ResolvedRequestMethod = strings.ToUpper(strings.TrimSpace(applyRouteTemplate(route.Target.Method, info)))
		}
		info.ResolvedRequestPath = targetPath
		info.ResolvedRequestQuery = mergedQuery
		info.MatchedRestRouteName = strings.TrimSpace(route.Name)
		return
	}
}

func checkBodyFieldCondition(requestBodyJSON string, fieldPath string, shouldExist bool) bool {
	if fieldPath == "" {
		return true
	}
	if requestBodyJSON == "" {
		// no body available, skip body conditions
		return true
	}
	exists := gjson.Get(requestBodyJSON, fieldPath).Exists()
	return exists == shouldExist
}

func ValidateRestRouteRules(routes []dto.RestRouteRule) error {
	for index, route := range routes {
		line := index + 1
		if !route.Enabled {
			continue
		}
		if strings.TrimSpace(route.Target.Path) == "" {
			return fmt.Errorf("rest_routes[%d].target.path is required", line-1)
		}
		if route.Target.Query != nil {
			for key := range route.Target.Query {
				if strings.TrimSpace(key) == "" {
					return fmt.Errorf("rest_routes[%d].target.query contains empty key", line-1)
				}
			}
		}
		method := strings.TrimSpace(route.Target.Method)
		if method != "" && !isHTTPMethod(method) {
			return fmt.Errorf("rest_routes[%d].target.method is invalid", line-1)
		}
		matchMethod := strings.TrimSpace(route.Match.Method)
		if matchMethod != "" && !isHTTPMethod(matchMethod) {
			return fmt.Errorf("rest_routes[%d].match.method is invalid", line-1)
		}
		if strings.TrimSpace(route.Match.RequestPath) == "" &&
			strings.TrimSpace(route.Match.RequestPathPrefix) == "" &&
			strings.TrimSpace(route.Match.Method) == "" &&
			strings.TrimSpace(route.Match.RelayMode) == "" &&
			strings.TrimSpace(route.Match.ModelPrefix) == "" &&
			strings.TrimSpace(route.Match.RequestBodyFieldExists) == "" &&
			strings.TrimSpace(route.Match.RequestBodyFieldMissing) == "" {
			return fmt.Errorf("rest_routes[%d].match must contain at least one condition", line-1)
		}
	}
	return nil
}

func isHTTPMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return true
	default:
		return false
	}
}

func BuildResolvedURL(baseURL string, requestPath string, extraQuery map[string]string) string {
	trimmedBase := strings.TrimSpace(baseURL)
	if trimmedBase == "" {
		return requestPath
	}
	fullURL := appcommon.BuildURL(trimmedBase, requestPath)
	if len(extraQuery) == 0 {
		return fullURL
	}
	parsed, err := url.Parse(fullURL)
	if err != nil {
		return fullURL
	}
	values := parsed.Query()
	for key, value := range extraQuery {
		values.Set(key, value)
	}
	parsed.RawQuery = values.Encode()
	return parsed.String()
}
