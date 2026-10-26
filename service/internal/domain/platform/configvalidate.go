package platform

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"xsite/service/internal/domain/ops"
)

// 导入的条数上限。备份是人工维护的配置，量级很小；设上限是为了让一份异常的大文件早早被拒掉，
// 而不是在事务里跑很久再失败。
const (
	maxBackupTaxonomies = 200
	maxBackupTerms      = 2000 // 单个维度下的取值
	maxBackupAds        = 2000
)

const (
	maxSlugLen = 64
	maxNameLen = 64
)

// slug 与 code 的格式与内容管理页一致：小写字母、数字、连字符。
var backupSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// 允许的枚举取值。导入的数据最终会撞数据库的 CHECK 约束，提前在这里挡住是为了给出看得懂的提示，
// 而不是把 PG 的约束名抛给运维。
//
// 取值从运营域的常量展开，不在这里抄一份：以后新增广告位时这里会自动跟上，
// 否则一份合法的备份会被过时的白名单拒掉。
var (
	backupStatuses      = []string{"active", "inactive"}
	backupAdSlots       = enumStrings(ops.AdSlots)
	backupCreativeTypes = enumStrings([]ops.CreativeType{ops.CreativeImage, ops.CreativeVideo, ops.CreativeText, ops.CreativeHTML})
	backupAdDevices     = enumStrings([]ops.AdDevice{ops.AdDeviceAll, ops.AdDeviceMobile, ops.AdDeviceDesktop})
)

// enumStrings 把领域里的枚举类型转成字符串切片。
func enumStrings[T ~string](list []T) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = string(v)
	}
	return out
}

// validateBackupData 校验备份正文。只检查结构与取值范围，不碰库。
func validateBackupData(d BackupData) error {
	if n := len(d.Taxonomies); n > maxBackupTaxonomies {
		return badRequest("备份里有 %d 个筛选维度，超过上限 %d", n, maxBackupTaxonomies)
	}
	if n := len(d.Ads); n > maxBackupAds {
		return badRequest("备份里有 %d 条广告，超过上限 %d", n, maxBackupAds)
	}

	// 维度 slug 全局唯一；频道取值的 slug 与非空的 code 也都是全站唯一（见迁移 00004 的部分唯一索引）
	seenTaxonomy := map[string]bool{}
	seenChannel := map[string]bool{}
	seenCode := map[string]bool{}
	for i, t := range d.Taxonomies {
		where := func() string { return "第 " + strconv.Itoa(i+1) + " 个筛选维度" }
		if err := checkName(t.Name, where()+"的名称"); err != nil {
			return err
		}
		if err := checkSlug(t.Slug, where()+"的标识"); err != nil {
			return err
		}
		if seenTaxonomy[t.Slug] {
			return badRequest("备份里有两个筛选维度用了同一个标识 %q", t.Slug)
		}
		seenTaxonomy[t.Slug] = true
		if !slices.Contains(backupStatuses, t.Status) {
			return badRequest("%s的状态 %q 无效（只能是 active 或 inactive）", where(), t.Status)
		}
		if n := len(t.Terms); n > maxBackupTerms {
			return badRequest("维度 %q 下有 %d 个取值，超过上限 %d", t.Slug, n, maxBackupTerms)
		}

		seenTerm := map[string]bool{}
		for j, term := range t.Terms {
			tw := "维度 " + t.Slug + " 的第 " + strconv.Itoa(j+1) + " 个取值"
			if err := checkName(term.Name, tw+"的名称"); err != nil {
				return err
			}
			if err := checkSlug(term.Slug, tw+"的标识"); err != nil {
				return err
			}
			if seenTerm[term.Slug] {
				return badRequest("维度 %q 下有两个取值用了同一个标识 %q", t.Slug, term.Slug)
			}
			seenTerm[term.Slug] = true
			if !slices.Contains(backupStatuses, term.Status) {
				return badRequest("%s的状态 %q 无效（只能是 active 或 inactive）", tw, term.Status)
			}
			// 频道取值有独立的前台地址 /c/:slug，所以 slug 要跨维度唯一
			if term.IsChannel {
				if seenChannel[term.Slug] {
					return badRequest("备份里有两个频道取值用了同一个标识 %q（频道标识要全站唯一）", term.Slug)
				}
				seenChannel[term.Slug] = true
			}
			if term.Code != "" {
				if err := checkSlug(term.Code, tw+"的编码"); err != nil {
					return err
				}
				if seenCode[term.Code] {
					return badRequest("备份里有两个取值用了同一个编码 %q（编码要全站唯一）", term.Code)
				}
				seenCode[term.Code] = true
			}
		}
	}

	for i, a := range d.Ads {
		where := "第 " + strconv.Itoa(i+1) + " 条广告"
		if err := checkName(a.Name, where+"的名称"); err != nil {
			return err
		}
		if !slices.Contains(backupAdSlots, a.Slot) {
			return badRequest("%s的广告位 %q 无效", where, a.Slot)
		}
		if !slices.Contains(backupCreativeTypes, a.CreativeType) {
			return badRequest("%s的素材类型 %q 无效（只能是 image、video、text 或 html）", where, a.CreativeType)
		}
		if !slices.Contains(backupAdDevices, a.Device) {
			return badRequest("%s的投放端 %q 无效（只能是 all、desktop、mobile 或 app）", where, a.Device)
		}
		if !slices.Contains(backupStatuses, a.Status) {
			return badRequest("%s的状态 %q 无效（只能是 active 或 inactive）", where, a.Status)
		}
		// 数据库有表级 CHECK 要求素材类型对应的字段非空，提前挡住给出明确提示
		switch a.CreativeType {
		case "image":
			if a.Image == "" {
				return badRequest("%s是图片素材，但没有图片地址", where)
			}
		case "video":
			if a.VideoURL == "" {
				return badRequest("%s是视频素材，但没有视频地址", where)
			}
		case "text":
			if a.Text == "" {
				return badRequest("%s是文字素材，但没有文字内容", where)
			}
		case "html":
			if a.HTML == "" {
				return badRequest("%s是自定义 HTML 素材，但没有 HTML 内容", where)
			}
		}
		if a.Weight < 0 {
			return badRequest("%s的权重不能是负数", where)
		}
		if a.FreqCap < 0 {
			return badRequest("%s的每日曝光上限不能是负数", where)
		}
	}
	return nil
}

func checkSlug(s, where string) error {
	if s == "" {
		return badRequest("%s不能为空", where)
	}
	if len(s) > maxSlugLen {
		return badRequest("%s不能超过 %d 个字符", where, maxSlugLen)
	}
	if !backupSlugRe.MatchString(s) {
		return badRequest("%s %q 只能用小写字母、数字和连字符，且以字母或数字开头", where, s)
	}
	return nil
}

func checkName(s, where string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return badRequest("%s不能为空", where)
	}
	if utf8.RuneCountInString(s) > maxNameLen {
		return badRequest("%s不能超过 %d 个字", where, maxNameLen)
	}
	return nil
}
