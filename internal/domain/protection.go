package domain

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

type WAFMode string

const (
	WAFModeDisabled      WAFMode = "disabled"
	WAFModeDetectionOnly WAFMode = "detection_only"
	WAFModeBlocking      WAFMode = "blocking"
)

type RateLimitPreset string

const (
	RateLimitPresetNormal RateLimitPreset = "normal"
	RateLimitPresetStrict RateLimitPreset = "strict"
	RateLimitPresetCustom RateLimitPreset = "custom"
)

type ProtectionConfig struct {
	WAF       WAFConfig       `json:"waf"`
	RateLimit RateLimitConfig `json:"rate_limit"`
	IPAccess  IPAccessConfig  `json:"ip_access"`
	AutoBan   AutoBanConfig   `json:"auto_ban"`
}

type WAFConfig struct {
	Mode            WAFMode            `json:"mode"`
	ParanoiaLevel   int                `json:"paranoia_level"`
	ExcludedRuleIDs []int              `json:"excluded_rule_ids"`
	PathExclusions  []WAFPathExclusion `json:"path_exclusions"`
	CustomRules     string             `json:"custom_rules"`
}

type WAFPathExclusion struct {
	Path            string `json:"path"`
	DisableWAF      bool   `json:"disable_waf"`
	ExcludedRuleIDs []int  `json:"excluded_rule_ids"`
}

type RateLimitConfig struct {
	Enabled           bool            `json:"enabled"`
	Preset            RateLimitPreset `json:"preset"`
	RequestsPerMinute int             `json:"requests_per_minute"`
}

type IPAccessConfig struct {
	Allowed []string `json:"allowed"`
	Blocked []string `json:"blocked"`
}

type AutoBanConfig struct {
	Enabled         bool `json:"enabled"`
	BlockedRequests int  `json:"blocked_requests"`
	WindowMinutes   int  `json:"window_minutes"`
	BanMinutes      int  `json:"ban_minutes"`
}

type UpdateProtectionInput struct {
	Protection ProtectionConfig `json:"protection_config"`
}

func NormalizeProtectionConfig(config ProtectionConfig) ProtectionConfig {
	config.WAF.Mode = normalizeWAFMode(config.WAF.Mode)
	if config.WAF.ParanoiaLevel < 1 || config.WAF.ParanoiaLevel > 4 {
		config.WAF.ParanoiaLevel = 1
	}
	config.WAF.ExcludedRuleIDs = normalizeRuleIDs(config.WAF.ExcludedRuleIDs)
	config.WAF.CustomRules = strings.TrimSpace(config.WAF.CustomRules)
	config.WAF.PathExclusions = normalizeWAFPathExclusions(config.WAF.PathExclusions)

	config.RateLimit.Preset = normalizeRateLimitPreset(config.RateLimit.Preset)
	if config.RateLimit.RequestsPerMinute <= 0 {
		switch config.RateLimit.Preset {
		case RateLimitPresetStrict:
			config.RateLimit.RequestsPerMinute = 60
		default:
			config.RateLimit.RequestsPerMinute = 120
		}
	}

	config.IPAccess.Allowed = normalizeIPList(config.IPAccess.Allowed)
	config.IPAccess.Blocked = normalizeIPList(config.IPAccess.Blocked)

	if config.AutoBan.BlockedRequests <= 0 {
		config.AutoBan.BlockedRequests = 20
	}
	if config.AutoBan.WindowMinutes <= 0 {
		config.AutoBan.WindowMinutes = 10
	}
	if config.AutoBan.BanMinutes <= 0 {
		config.AutoBan.BanMinutes = 60
	}

	return config
}

func ValidateProtectionConfig(config ProtectionConfig) ValidationErrors {
	validation := ValidationErrors{}

	switch config.WAF.Mode {
	case WAFModeDisabled, WAFModeDetectionOnly, WAFModeBlocking:
	default:
		validation["waf.mode"] = "Select a valid WAF mode."
	}
	if config.WAF.ParanoiaLevel < 1 || config.WAF.ParanoiaLevel > 4 {
		validation["waf.paranoia_level"] = "Paranoia level must be between 1 and 4."
	}

	if len(config.WAF.CustomRules) > 20000 {
		validation["waf.custom_rules"] = "Custom rules must be 20,000 characters or less."
	}
	if len(config.WAF.ExcludedRuleIDs) > 200 {
		validation["waf.excluded_rule_ids"] = "Enter 200 excluded rule IDs or fewer."
	}
	for _, ruleID := range config.WAF.ExcludedRuleIDs {
		if ruleID <= 0 {
			validation["waf.excluded_rule_ids"] = "Rule IDs must be positive numbers."
			break
		}
	}
	if len(config.WAF.PathExclusions) > 100 {
		validation["waf.path_exclusions"] = "Enter 100 path exclusions or fewer."
	}
	for index, exclusion := range config.WAF.PathExclusions {
		field := fmt.Sprintf("waf.path_exclusions.%d.path", index)
		if message := validateSecurityPath(exclusion.Path); message != "" {
			validation[field] = message
		}
		if len(exclusion.ExcludedRuleIDs) > 50 {
			validation[fmt.Sprintf("waf.path_exclusions.%d.excluded_rule_ids", index)] = "Enter 50 rule IDs or fewer for one path."
		}
		if !exclusion.DisableWAF && len(exclusion.ExcludedRuleIDs) == 0 {
			validation[fmt.Sprintf("waf.path_exclusions.%d.excluded_rule_ids", index)] = "Enter at least one rule ID or disable WAF for this path."
		}
		for _, ruleID := range exclusion.ExcludedRuleIDs {
			if ruleID <= 0 {
				validation[fmt.Sprintf("waf.path_exclusions.%d.excluded_rule_ids", index)] = "Rule IDs must be positive numbers."
				break
			}
		}
	}

	validateIPList(validation, "ip_access.allowed", config.IPAccess.Allowed)
	validateIPList(validation, "ip_access.blocked", config.IPAccess.Blocked)

	switch config.RateLimit.Preset {
	case RateLimitPresetNormal, RateLimitPresetStrict, RateLimitPresetCustom:
	default:
		validation["rate_limit.preset"] = "Select a valid rate-limit preset."
	}
	if config.RateLimit.Enabled && (config.RateLimit.RequestsPerMinute < 1 || config.RateLimit.RequestsPerMinute > 10000) {
		validation["rate_limit.requests_per_minute"] = "Requests per minute must be between 1 and 10000."
	}
	if config.AutoBan.Enabled {
		if config.WAF.Mode != WAFModeBlocking && !config.RateLimit.Enabled {
			validation["auto_ban.enabled"] = "Auto-ban requires blocking WAF or rate limiting."
		}
		if config.AutoBan.BlockedRequests < 1 || config.AutoBan.BlockedRequests > 10000 {
			validation["auto_ban.blocked_requests"] = "Blocked requests must be between 1 and 10000."
		}
		if config.AutoBan.WindowMinutes < 1 || config.AutoBan.WindowMinutes > 1440 {
			validation["auto_ban.window_minutes"] = "Window must be between 1 and 1440 minutes."
		}
		if config.AutoBan.BanMinutes < 1 || config.AutoBan.BanMinutes > 10080 {
			validation["auto_ban.ban_minutes"] = "Ban duration must be between 1 and 10080 minutes."
		}
	}

	return validation
}

func normalizeWAFMode(mode WAFMode) WAFMode {
	switch mode {
	case WAFModeDetectionOnly, WAFModeBlocking:
		return mode
	default:
		return WAFModeDisabled
	}
}

func normalizeRateLimitPreset(preset RateLimitPreset) RateLimitPreset {
	switch preset {
	case RateLimitPresetStrict, RateLimitPresetCustom:
		return preset
	default:
		return RateLimitPresetNormal
	}
}

func normalizeRuleIDs(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	ids := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		ids = append(ids, value)
	}
	sort.Ints(ids)
	return ids
}

func normalizeWAFPathExclusions(values []WAFPathExclusion) []WAFPathExclusion {
	exclusions := make([]WAFPathExclusion, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		path := strings.TrimSpace(value.Path)
		if path == "" {
			continue
		}
		ids := normalizeRuleIDs(value.ExcludedRuleIDs)
		if !value.DisableWAF && len(ids) == 0 {
			continue
		}
		key := fmt.Sprintf("%s|%t|%v", path, value.DisableWAF, ids)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		exclusions = append(exclusions, WAFPathExclusion{
			Path:            path,
			DisableWAF:      value.DisableWAF,
			ExcludedRuleIDs: ids,
		})
	}
	return exclusions
}

func normalizeIPList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	ips := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		ips = append(ips, value)
	}
	sort.Strings(ips)
	return ips
}

func validateSecurityPath(path string) string {
	if path == "" {
		return "Path is required."
	}
	if !strings.HasPrefix(path, "/") {
		return "Path must start with /."
	}
	if len(path) > 2048 {
		return "Path must be 2048 characters or less."
	}
	if strings.ContainsAny(path, "\n\r\t") {
		return "Path must be a single line."
	}
	return ""
}

func validateIPList(validation ValidationErrors, field string, values []string) {
	if len(values) > 256 {
		validation[field] = "Enter 256 IPs or CIDR ranges or fewer."
		return
	}
	for _, value := range values {
		if _, err := netip.ParseAddr(value); err == nil {
			continue
		}
		if _, err := netip.ParsePrefix(value); err == nil {
			continue
		}
		validation[field] = "Enter valid IP addresses or CIDR ranges."
		return
	}
}
