package moderation

import (
	"context"
	"strings"
	"time"

	"xsite/service/internal/errs"
)

const (
	maxDanmakuRunes     = 40    // 弹幕最大字数（docs/03 第 5.2 节）
	danmakuWindowMs     = 60000 // 单次拉取的时间窗口上限：60 秒
	danmakuWindowMax    = 300   // 单窗口最多返回条数
	defaultDanmakuColor = "FFFFFF"
)

// danmakuColors 是允许的弹幕颜色白名单（RRGGBB，大写）。超出白名单的颜色拒绝发送。
var danmakuColors = map[string]bool{
	"FFFFFF": true, // 白
	"000000": true, // 黑
	"FF0000": true, // 红
	"FF7F00": true, // 橙
	"FFFF00": true, // 黄
	"00FF00": true, // 绿
	"00FFFF": true, // 青
	"0000FF": true, // 蓝
	"FF00FF": true, // 品红
	"9B59B6": true, // 紫
}

// Danmaku 是一条弹幕（领域模型 / 发送后回执）。
type Danmaku struct {
	ID        int64     `json:"id"`
	EpisodeID int64     `json:"episode_id"`
	TimeMS    int       `json:"time_ms"`
	Content   string    `json:"content"`
	Color     string    `json:"color"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// DanmakuView 是按窗口拉取时的一条弹幕（仅播放器需要的最小字段）。
type DanmakuView struct {
	ID      int64  `json:"id"`
	TimeMS  int    `json:"time_ms"`
	Content string `json:"content"`
	Color   string `json:"color"`
}

// NewDanmaku 是要创建的弹幕（仓储层用）。
type NewDanmaku struct {
	EpisodeID int64
	Author    Author
	TimeMS    int
	Content   string
	Color     string
	Status    string
}

// DanmakuRepo 读写弹幕表（实现见 internal/store/pg）。
type DanmakuRepo interface {
	Create(ctx context.Context, in NewDanmaku) (Danmaku, error)
	// ListWindow 返回某集 [fromMS, toMS) 窗口内显示中的弹幕，按时间升序，最多 limit 条。
	ListWindow(ctx context.Context, episodeID int64, fromMS, toMS, limit int) ([]DanmakuView, error)
}

// DanmakuService 处理弹幕发送与按窗口拉取（docs/03 第 5.2 节）。
// 弹幕不做重复内容去重（与评论不同）；先审后显仅由命中 review 词触发。
type DanmakuService struct {
	repo     DanmakuRepo
	eval     Evaluator
	features Features
	users    Users
}

func NewDanmakuService(repo DanmakuRepo, eval Evaluator, features Features, users Users) *DanmakuService {
	return &DanmakuService{repo: repo, eval: eval, features: features, users: users}
}

// SendDanmakuInput 是发送弹幕的入参。
type SendDanmakuInput struct {
	Author    Author
	EpisodeID int64
	TimeMS    int
	Content   string
	Color     string
}

// Send 发送弹幕：过功能开关 / 禁言 / 清洗 / 字数 / 颜色白名单 / 屏蔽词 / 先审后显。
func (s *DanmakuService) Send(ctx context.Context, in SendDanmakuInput) (Danmaku, error) {
	if err := authorCanPost(ctx, s.features, s.users, in.Author, FlagDanmaku); err != nil {
		return Danmaku{}, err
	}
	if in.TimeMS < 0 {
		return Danmaku{}, errs.ErrBadRequest.WithMessage("弹幕时间无效")
	}
	// 弹幕是单行文本：换行折成空格。
	content := strings.TrimSpace(strings.ReplaceAll(cleanContent(in.Content), "\n", " "))
	if content == "" {
		return Danmaku{}, errs.ErrBadRequest.WithMessage("弹幕内容不能为空")
	}
	if runeLen(content) > maxDanmakuRunes {
		return Danmaku{}, errs.ErrBadRequest.WithMessage("弹幕最多 40 个字")
	}
	color, ok := normalizeColor(in.Color)
	if !ok {
		return Danmaku{}, errs.ErrBadRequest.WithMessage("弹幕颜色不被支持")
	}
	d := s.eval.Evaluate(content)
	if d.Blocked {
		return Danmaku{}, errBlocked
	}
	status := StatusVisible
	if d.Review {
		status = StatusPending
	}
	return s.repo.Create(ctx, NewDanmaku{
		EpisodeID: in.EpisodeID, Author: in.Author, TimeMS: in.TimeMS,
		Content: d.Text, Color: color, Status: status,
	})
}

// Window 返回某集一个时间窗口内显示中的弹幕。窗口自动夹到 60 秒内、最多 300 条。
func (s *DanmakuService) Window(ctx context.Context, episodeID int64, fromMS, toMS int) ([]DanmakuView, error) {
	if episodeID <= 0 {
		return nil, errs.ErrBadRequest.WithMessage("分集 ID 无效")
	}
	if fromMS < 0 {
		fromMS = 0
	}
	if toMS <= fromMS || toMS-fromMS > danmakuWindowMs {
		toMS = fromMS + danmakuWindowMs
	}
	items, err := s.repo.ListWindow(ctx, episodeID, fromMS, toMS, danmakuWindowMax)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []DanmakuView{}
	}
	return items, nil
}

// normalizeColor 规范化颜色：去前导 #、转大写；空值回落到默认白；非白名单返回 ok=false。
func normalizeColor(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return defaultDanmakuColor, true
	}
	s = strings.ToUpper(strings.TrimPrefix(s, "#"))
	if danmakuColors[s] {
		return s, true
	}
	return "", false
}
