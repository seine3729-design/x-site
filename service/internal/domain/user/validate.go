package user

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"xsite/service/internal/errs"
)

// usernameRe 限定用户名字符集；库中统一存小写（docs/03 第 2.2 节），展示名用 nickname。
var usernameRe = regexp.MustCompile(`^[a-z0-9_]{3,32}$`)

// reservedUsername 是保留的用户名，避免冒充官方或与系统标识混淆。
var reservedUsername = map[string]bool{
	"admin": true, "administrator": true, "root": true, "system": true, "official": true,
	"staff": true, "support": true, "service": true, "null": true, "undefined": true, "guest": true,
}

// commonWeakPassword 是一组最常见的弱口令（docs/03 第 2.2 节：校验常见弱口令表）。
// 这里只收最高频、长度达标的若干个；更系统的字典与撞库防护随 S2-2 评估接入。
var commonWeakPassword = map[string]bool{
	"password": true, "passw0rd": true, "password1": true, "password123": true, "12345678": true,
	"123456789": true, "1234567890": true, "qwerty123": true, "qwertyuiop": true, "11111111": true,
	"00000000": true, "abc12345": true, "iloveyou": true, "admin123": true, "letmein1": true,
	"welcome1": true, "sunshine": true, "princess": true, "football": true, "baseball": true,
	"88888888": true, "a1b2c3d4": true, "zxcvbnm1": true, "asdfghjkl": true, "1q2w3e4r": true,
	"qazwsxedc": true, "changeme": true, "whatever": true, "trustno1": true, "superman": true,
}

func normalizeUsername(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// validateUsername 校验并返回规范化（小写）后的用户名。
func validateUsername(s string) (string, error) {
	u := normalizeUsername(s)
	if !usernameRe.MatchString(u) {
		return "", errs.ErrBadRequest.WithMessage("用户名只能用 3–32 位小写字母、数字或下划线")
	}
	if reservedUsername[u] {
		return "", errs.ErrBadRequest.WithMessage("该用户名已被保留，请换一个")
	}
	return u, nil
}

// validatePassword 校验密码强度：8–128 位、非纯数字、不在常见弱口令表、不等于用户名。
// username 为空时跳过「不能与用户名相同」检查（改密时无需比对用户名）。
func validatePassword(p, username string) error {
	n := utf8.RuneCountInString(p)
	if n < 8 || n > 128 {
		return errs.ErrBadRequest.WithMessage("密码长度需在 8 到 128 位之间")
	}
	lower := strings.ToLower(p)
	if commonWeakPassword[lower] {
		return errs.ErrBadRequest.WithMessage("密码过于常见，请换一个更复杂的")
	}
	if username != "" && lower == username {
		return errs.ErrBadRequest.WithMessage("密码不能与用户名相同")
	}
	if isAllDigits(p) {
		return errs.ErrBadRequest.WithMessage("密码不能是纯数字")
	}
	return nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// validateNickname 校验并规范化昵称：去控制字符、折叠空白，≤ 40 字；允许为空（展示时回退用户名）。
func validateNickname(s string) (string, error) {
	n := cleanText(s)
	if utf8.RuneCountInString(n) > 40 {
		return "", errs.ErrBadRequest.WithMessage("昵称最多 40 个字")
	}
	return n, nil
}

// cleanText 规范化单行文本：控制字符换空格、折叠连续空白、裁剪首尾。
func cleanText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// validateBio 校验并规范化个人简介：去控制字符（保留换行）、逐行折叠空白、最多 2 个换行、≤ 80 字。
// S2-3 只做基本校验；屏蔽词与引流识别（网址 / 联系方式）随 S2-9。
func validateBio(s string) (string, error) {
	b := cleanMultiline(s)
	if strings.Count(b, "\n") > 2 {
		return "", errs.ErrBadRequest.WithMessage("简介最多 2 个换行")
	}
	if utf8.RuneCountInString(b) > 80 {
		return "", errs.ErrBadRequest.WithMessage("简介最多 80 个字")
	}
	return b, nil
}

// validateAvatar 校验头像引用（预设头像 key 或已上传对象 key）。S2-3 只做长度与控制字符检查；
// 头像库与上传来源校验随 S2-9 / S6-13。
func validateAvatar(s string) (string, error) {
	a := strings.TrimSpace(s)
	if utf8.RuneCountInString(a) > 500 {
		return "", errs.ErrBadRequest.WithMessage("头像地址过长")
	}
	for _, r := range a {
		if unicode.IsControl(r) {
			return "", errs.ErrBadRequest.WithMessage("头像地址含非法字符")
		}
	}
	return a, nil
}

// cleanMultiline 规范化多行文本：统一换行、丢弃控制字符（除换行）、逐行折叠空白并裁剪、去掉首尾空行。
func cleanMultiline(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimSpace(strings.Join(strings.Fields(ln), " "))
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
