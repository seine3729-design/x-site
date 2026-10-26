package config

import (
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
)

// 资源机账号密码：FTP_<凭据标识>_USER、FTP_<凭据标识>_PASSWORD（docs/specs/S1-6.md）。
// 凭据标识是后台登记资源机时的 cred_ref，账号密码不入库，线上由密钥管理注入 worker。
var ftpCredEnvRe = regexp.MustCompile(`^FTP_([A-Z][A-Z0-9_]*)_(USER|PASSWORD)$`)

// FTPCredentials 按凭据标识保存资源机账号密码。只有 worker 用到；
// String、GoString 与 LogValue 只输出数量，防止误打进日志。
type FTPCredentials struct {
	m map[string]ftpCredential
}

type ftpCredential struct {
	user, password string
}

// Lookup 返回凭据标识对应的账号与密码；没有配置时 ok 为 false。
func (c FTPCredentials) Lookup(ref string) (user, password string, ok bool) {
	v, ok := c.m[ref]
	return v.user, v.password, ok
}

// Refs 返回已配置的凭据标识（排序后）。
func (c FTPCredentials) Refs() []string {
	refs := make([]string, 0, len(c.m))
	for ref := range c.m {
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	return refs
}

func (c FTPCredentials) String() string       { return fmt.Sprintf("FTPCredentials(%d)", len(c.m)) }
func (c FTPCredentials) GoString() string     { return c.String() }
func (c FTPCredentials) LogValue() slog.Value { return slog.IntValue(len(c.m)) }

// parseFTPCredentials 从环境变量（os.Environ 格式）中取出资源机账号密码。账号与密码必须成对配置，
// 只配了一个（或值为空）时返回问题说明，进程启动失败而不是带着残缺的凭据运行。
func parseFTPCredentials(environ []string) (FTPCredentials, []string) {
	found := map[string]*ftpCredential{}
	seen := map[string]map[string]bool{} // 凭据标识 → 出现过的 USER / PASSWORD
	for _, kv := range environ {
		name, value, _ := strings.Cut(kv, "=")
		m := ftpCredEnvRe.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		ref, kind := m[1], m[2]
		if found[ref] == nil {
			found[ref], seen[ref] = &ftpCredential{}, map[string]bool{}
		}
		if value == "" {
			continue
		}
		seen[ref][kind] = true
		if kind == "USER" {
			found[ref].user = value
		} else {
			found[ref].password = value
		}
	}

	out := FTPCredentials{m: map[string]ftpCredential{}}
	var problems []string
	for ref, cred := range found {
		hasUser, hasPassword := seen[ref]["USER"], seen[ref]["PASSWORD"]
		switch {
		case hasUser && hasPassword:
			out.m[ref] = *cred
		case hasUser:
			problems = append(problems, fmt.Sprintf("已设置 FTP_%s_USER 但缺少 FTP_%s_PASSWORD（资源机账号与密码要成对配置）", ref, ref))
		case hasPassword:
			problems = append(problems, fmt.Sprintf("已设置 FTP_%s_PASSWORD 但缺少 FTP_%s_USER（资源机账号与密码要成对配置）", ref, ref))
		}
	}
	slices.Sort(problems)
	return out, problems
}
