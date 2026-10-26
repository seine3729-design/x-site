package collector

import (
	"fmt"
	"net/url"
	"strings"
)

// Robots 是针对本采集器 User-Agent 解析出的基础 robots 规则。
// 为避免把不支持的通配规则误判为允许，包含 * 或 $ 的选中规则会被拒绝。
type Robots struct {
	rules []robotsRule
}

type robotsRule struct {
	allow bool
	path  string
}

// ParseRobots 解析适用于 userAgent 的 Allow / Disallow 规则。
// 无匹配规则时按 robots 标准默认允许；robots 文件无法取得时由调用方失败关闭。
func ParseRobots(body []byte, userAgent string) (Robots, error) {
	agent := strings.ToLower(strings.TrimSpace(userAgent))
	if agent == "" {
		return Robots{}, fmt.Errorf("采集器 User-Agent 不能为空")
	}
	groups := map[string][]robotsRule{}
	var active []string
	hasRule := false
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if index := strings.IndexByte(line, '#'); index >= 0 {
			line = strings.TrimSpace(line[:index])
		}
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "user-agent":
			if hasRule {
				active = nil
				hasRule = false
			}
			if value != "" {
				active = append(active, strings.ToLower(value))
			}
		case "allow", "disallow":
			if len(active) == 0 {
				continue
			}
			hasRule = true
			if value == "" {
				continue // 空 Disallow 表示没有限制。
			}
			if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, "*$") {
				return Robots{}, fmt.Errorf("robots 规则 %q 不受支持，为避免误抓已拒绝", value)
			}
			rule := robotsRule{allow: key == "allow", path: value}
			for _, current := range active {
				groups[current] = append(groups[current], rule)
			}
		}
	}
	var rules []robotsRule
	bestAgentLength := -1
	for candidate, candidateRules := range groups {
		if candidate == "*" || !strings.HasPrefix(agent, candidate) || len(candidate) < bestAgentLength {
			continue
		}
		bestAgentLength = len(candidate)
		rules = candidateRules
	}
	if len(rules) == 0 {
		rules = groups["*"]
	}
	return Robots{rules: rules}, nil
}

// Allowed 返回请求路径是否可以抓取。最长匹配规则优先，长度相同 Allow 优先。
func (r Robots) Allowed(requestURL string) bool {
	u, err := url.Parse(requestURL)
	if err != nil {
		return false
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	bestLength, allowed := -1, true
	for _, rule := range r.rules {
		if !strings.HasPrefix(path, rule.path) {
			continue
		}
		if length := len(rule.path); length > bestLength || (length == bestLength && rule.allow) {
			bestLength, allowed = length, rule.allow
		}
	}
	return allowed
}
