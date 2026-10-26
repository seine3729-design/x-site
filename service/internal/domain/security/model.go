// Package security 是运营后台「安全中心」（docs/06 第 39 节，A4-4 / S4-8）的只读聚合：
// 管理员登录记录（读 admin_login_log）、密钥轮换记录（运行时 keyring 的版本号）、风控概览。
// 纯只读页面：权限点只有 security.view，没有写接口。
package security

import (
	"strings"
	"time"
)

// 登录结果枚举（admin_login_log.result，docs/02 第 6.1 节）。
var resultTitles = map[string]string{
	"success":      "成功",
	"bad_password": "密码错误",
	"inactive":     "账号停用",
	"mfa_failed":   "二次验证失败",
	"locked":       "账号锁定",
}

func validResult(r string) bool { _, ok := resultTitles[r]; return ok }

func resultTitle(r string) string {
	if t, ok := resultTitles[r]; ok {
		return t
	}
	return r
}

// LoginLog 是一条管理员登录记录。CurrentUsername 是 admin_id 关联到的当前账号用户名（账号已删时为空）。
type LoginLog struct {
	ID              int64     `json:"id"`
	AdminID         *int64    `json:"admin_id"`
	Username        string    `json:"username"`         // 本次登录尝试用的用户名
	CurrentUsername string    `json:"current_username"` // 关联账号的当前用户名
	Result          string    `json:"result"`
	ResultTitle     string    `json:"result_title"`
	Suspicious      bool      `json:"suspicious"` // 失败类结果，异常登录告警据此标红
	IPPrefix        string    `json:"ip_prefix"`
	UADigest        string    `json:"ua_digest"`
	CreatedAt       time.Time `json:"created_at"`
}

// LoginLogFilter 是登录记录的筛选（页码分页）。Pattern 由 service 填好。
type LoginLogFilter struct {
	Result   string
	Q        string
	Pattern  string
	Since    *time.Time
	Until    *time.Time
	Page     int
	PageSize int
}

// LoginLogPage 是登录记录的一页。
type LoginLogPage struct {
	List     []LoginLog `json:"list"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
	// Results 是全部登录结果及中文名，供前端筛选下拉。
	Results []ResultInfo `json:"results"`
}

// ResultInfo 描述一种登录结果，页面做筛选下拉与图例用。
type ResultInfo struct {
	Result string `json:"result"`
	Title  string `json:"title"`
}

func resultInfos() []ResultInfo {
	// 固定顺序：成功在前，失败类在后。
	order := []string{"success", "bad_password", "mfa_failed", "locked", "inactive"}
	out := make([]ResultInfo, 0, len(order))
	for _, r := range order {
		out = append(out, ResultInfo{Result: r, Title: resultTitles[r]})
	}
	return out
}

// KeyVersion 是一把密钥的版本记录（docs/06 第 39 节：只展示版本号与用途，不展示密钥内容）。
type KeyVersion struct {
	Purpose      string `json:"purpose"`
	PurposeTitle string `json:"purpose_title"`
	Version      string `json:"version"` // kid（密钥标识，非密钥本身）
	Primary      bool   `json:"primary"` // 是否当前签发 / 加密用（keyring 第一把）
}

// ParseKeyVersions 从 `kid:secret[,kid2:secret2]` 形式的密钥环配置里只取出 kid（丢弃密钥本身）。
// 第一个有效项是当前使用的密钥（primary）。配置为空时返回 nil（该类密钥不展示）。
func ParseKeyVersions(purpose, title, raw string) []KeyVersion {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []KeyVersion
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kid := part
		if i := strings.Index(part, ":"); i >= 0 {
			kid = strings.TrimSpace(part[:i])
		}
		if kid == "" {
			continue
		}
		out = append(out, KeyVersion{Purpose: purpose, PurposeTitle: title, Version: kid, Primary: len(out) == 0})
	}
	return out
}

// RiskOverview 是风控概览。目前实时计数源未接入，Items 给出指标口径、Note 说明情况。
type RiskOverview struct {
	Items []RiskItem `json:"items"`
	Note  string     `json:"note"`
}

// RiskItem 是风控的一个指标。Value 为 nil 表示该指标的实时计数暂未接入。
type RiskItem struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Value *int64 `json:"value"`
	Desc  string `json:"desc"`
}
