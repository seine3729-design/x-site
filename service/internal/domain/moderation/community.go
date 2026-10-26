package moderation

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"uuid"

	"xsite/service/internal/errs"
)

// 评论 / 弹幕 / 举报共用的作者身份、依赖接口与清洗助手（S2-5，docs/03 第 5 节）。

// 功能开关名（site_config.features，docs/06 第 36 节）。
const (
	FlagComments         = "comments"
	FlagDanmaku          = "danmaku"
	FlagGuestInteraction = "guest_interaction"
)

// Author 是评论 / 弹幕 / 举报的作者：注册用户（UserID>0）或游客（GuestID 非零），恰好一个。
type Author struct {
	UserID  int64
	GuestID uuid.UUID
}

// IsGuest 表示这是游客作者。
func (a Author) IsGuest() bool { return a.UserID == 0 }

// valid 报告身份是否恰好绑定一个主体（UserID 与 GuestID 恰好一个被设置）。
func (a Author) valid() bool {
	var zero uuid.UUID
	return (a.UserID > 0) != (a.GuestID != zero)
}

// Evaluator 过屏蔽词并给出判定（由屏蔽词 Service 实现，见 evaluate.go）。
type Evaluator interface {
	Evaluate(text string) Decision
}

// Features 读站点功能开关（site_config.features；未设置视为打开，与 ops.FeatureFlags 语义一致）。
type Features interface {
	Enabled(ctx context.Context, name string) (bool, error)
}

// Policy 读评论策略（site_config.comment_policy）。pre_moderation=true 时所有评论先审后显。
type Policy interface {
	PreModeration(ctx context.Context) (bool, error)
}

// Users 读用户账号状态（能否发表的判定）。
type Users interface {
	// Muted 报告该注册用户是否禁止发评论 / 弹幕。
	// status=muted 与 status=banned 都返回 true：封禁是比禁言更重的处置，
	// 不能因为 Access Token 还没过期就让被封禁的账号继续发言（docs/11 S2-7）。
	Muted(ctx context.Context, userID int64) (bool, error)
}

// Limiter 做「同作者短时间内重复内容」去重（Valkey SET NX，实现见 internal/store/cache）。
type Limiter interface {
	// FirstPost 报告 key 是否在窗口内首次出现；false 表示重复。
	FirstPost(ctx context.Context, key string) (bool, error)
}

// authorCanPost 做评论 / 弹幕发表前的公共校验：身份有效、功能开关、游客互动开关、禁言。
// feature 是该互动的功能开关名（comments / danmaku）。
func authorCanPost(ctx context.Context, features Features, users Users, author Author, feature string) error {
	if !author.valid() {
		return errs.ErrNeedLogin
	}
	if on, err := features.Enabled(ctx, feature); err != nil {
		return err
	} else if !on {
		return errs.ErrForbidden.WithMessage("该功能已关闭")
	}
	if author.IsGuest() {
		if on, err := features.Enabled(ctx, FlagGuestInteraction); err != nil {
			return err
		} else if !on {
			return errGuestNeedsIn
		}
		return nil
	}
	muted, err := users.Muted(ctx, author.UserID)
	if err != nil {
		return err
	}
	if muted {
		return errMuted
	}
	return nil
}

var (
	errMuted        = errs.ErrForbidden.WithMessage("你已被禁言，暂时不能发表")
	errGuestNeedsIn = errs.ErrNeedLogin.WithMessage("请先登录后再参与互动")
	errBlocked      = errs.ErrBadRequest.WithMessage("内容包含不允许发布的词")
	errDupContent   = errs.ErrConflict.WithMessage("请勿重复发送相同内容")
)

// cleanContent 规范化用户文本：去首尾空白、丢弃除换行外的控制字符、折叠多余空行。
// 富文本一律按纯文本处理（前台渲染时转义），表情以 [码] 形式留在文本里。
func cleanContent(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// runeLen 返回按用户感知计的长度（rune 数）。
func runeLen(s string) int { return utf8.RuneCountInString(s) }
