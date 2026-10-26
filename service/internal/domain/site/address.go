package site

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// 防丢地址「回家的路」（docs/04 第 14 节、docs/06 第 12 节）：存在 site_config.addresses 段，随站点配置下发。
// 本任务只做数据源；前台页面在客户端校验签名后展示，签名域名清单属于 S4-8。
const (
	maxBackupURLs  = 3
	maxSocials     = 10
	maxNoticeRunes = 200
	maxSocialName  = 20
)

// Social 是一个社交账号入口。
type Social struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Addresses 是防丢地址各项。全部可留空：留空表示前台不显示该项。
type Addresses struct {
	Notice       string   `json:"notice"`        // 提醒文字（如「请收藏永久地址」）
	LatestURL    string   `json:"latest_url"`    // 最新地址
	PermanentURL string   `json:"permanent_url"` // 永久地址
	BackupURLs   []string `json:"backup_urls"`   // 备用地址，最多 3 个
	PublishPage  string   `json:"publish_page"`  // 发布页
	SupportURL   string   `json:"support_url"`   // 客服链接
	Telegram     string   `json:"telegram"`      // Telegram
	Socials      []Social `json:"socials"`       // 其他社交账号
}

// AddressRepo 读写 site_config.addresses 段。
type AddressRepo interface {
	SiteAddresses(ctx context.Context) (Addresses, error)
	SetSiteAddresses(ctx context.Context, a Addresses) (Addresses, error)
}

// AddressService 是防丢地址的后台读写。保存后令站点配置版本 +1，各实例秒级生效。
type AddressService struct {
	repo    AddressRepo
	version VersionBumper
}

// NewAddressService 创建防丢地址服务。
func NewAddressService(repo AddressRepo, version VersionBumper) *AddressService {
	return &AddressService{repo: repo, version: version}
}

// Get 返回当前的防丢地址。
func (s *AddressService) Get(ctx context.Context) (Addresses, error) {
	a, err := s.repo.SiteAddresses(ctx)
	if err != nil {
		return Addresses{}, err
	}
	return a.normalized(), nil
}

// Set 整体保存防丢地址，返回修改前后的值与新的站点配置版本。
func (s *AddressService) Set(ctx context.Context, in Addresses) (before, after Addresses, version int64, err error) {
	if in, err = cleanAddresses(in); err != nil {
		return
	}
	if before, err = s.Get(ctx); err != nil {
		return
	}
	if after, err = s.repo.SetSiteAddresses(ctx, in); err != nil {
		return
	}
	after = after.normalized()
	// 地址随站点配置下发：版本 +1 并广播失效，失败则返回错误（否则前台会继续用旧地址）
	version, err = s.version.BumpVersion(ctx)
	return
}

// normalized 把 nil 列表换成空数组，JSON 下发为 [] 而不是 null。
func (a Addresses) normalized() Addresses {
	if a.BackupURLs == nil {
		a.BackupURLs = []string{}
	}
	if a.Socials == nil {
		a.Socials = []Social{}
	}
	return a
}

// JSON 实现供仓储直接写入 jsonb。
func (a Addresses) JSON() ([]byte, error) {
	b, err := json.Marshal(a.normalized())
	if err != nil {
		return nil, fmt.Errorf("序列化防丢地址: %w", err)
	}
	return b, nil
}

func cleanAddresses(in Addresses) (Addresses, error) {
	var err error
	if in.Notice, err = cleanText("提醒文字", in.Notice, maxNoticeRunes, false); err != nil {
		return in, err
	}
	for _, f := range []struct {
		label string
		value *string
	}{
		{"最新地址", &in.LatestURL},
		{"永久地址", &in.PermanentURL},
		{"发布页", &in.PublishPage},
		{"客服链接", &in.SupportURL},
		{"Telegram", &in.Telegram},
	} {
		if *f.value, err = cleanExternalURL(f.label, *f.value); err != nil {
			return in, err
		}
	}
	backups := make([]string, 0, len(in.BackupURLs))
	for i, raw := range in.BackupURLs {
		u, err := cleanExternalURL(fmt.Sprintf("备用地址 %d", i+1), raw)
		if err != nil {
			return in, err
		}
		if u == "" {
			continue
		}
		if len(backups) >= maxBackupURLs {
			return in, badRequest("备用地址最多 %d 个", maxBackupURLs)
		}
		backups = append(backups, u)
	}
	in.BackupURLs = backups
	socials := make([]Social, 0, len(in.Socials))
	for i, s := range in.Socials {
		name, err := cleanText(fmt.Sprintf("社交账号 %d 的名称", i+1), s.Name, maxSocialName, false)
		if err != nil {
			return in, err
		}
		url, err := cleanExternalURL(fmt.Sprintf("社交账号 %d 的链接", i+1), s.URL)
		if err != nil {
			return in, err
		}
		if name == "" && url == "" {
			continue // 整行都空的跳过（前端会留空行）
		}
		if name == "" || url == "" {
			return in, badRequest("社交账号 %d 的名称与链接都要填", i+1)
		}
		if len(socials) >= maxSocials {
			return in, badRequest("社交账号最多 %d 个", maxSocials)
		}
		socials = append(socials, Social{Name: name, URL: url})
	}
	in.Socials = socials
	return in, nil
}

// cleanExternalURL 校验可选的 http(s) 完整地址（防丢地址都是站外地址，不接受站内路径）。
func cleanExternalURL(label, raw string) (string, error) {
	v, err := cleanLink(label, raw)
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", nil
	}
	if strings.HasPrefix(v, "/") {
		return "", badRequest("%s要填 http:// 或 https:// 开头的完整地址", label)
	}
	return v, nil
}
