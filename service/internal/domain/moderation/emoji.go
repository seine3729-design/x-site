package moderation

import (
	"context"
	"strings"
	"time"

	"xsite/service/internal/errs"
)

const (
	maxEmojiPackNameRunes = 32
	maxEmojiCodeRunes     = 32
	maxEmojiImageRunes    = 500
)

// EmojiPack 是一个表情包（领域模型 / 后台维护单位）。
type EmojiPack struct {
	ID        int64
	Name      string
	Sort      int
	CreatedAt time.Time
}

// Emoji 是一个表情，属于某个表情包。code 是评论里的表情码（如 [微笑]），全局唯一。
type Emoji struct {
	ID        int64
	PackID    int64
	Code      string
	Image     string
	Sort      int
	CreatedAt time.Time
}

// EmojiPackView 是前台下发的一个表情包：包名 + 其表情（只给渲染需要的 code / image）。
type EmojiPackView struct {
	ID     int64       `json:"id"`
	Name   string      `json:"name"`
	Emojis []EmojiView `json:"emojis"`
}

// EmojiView 是前台下发的一个表情。
type EmojiView struct {
	Code  string `json:"code"`
	Image string `json:"image"`
}

// EmojiRepo 读写表情包 / 表情（实现见 internal/store/pg）。
type EmojiRepo interface {
	// ListPacks 返回所有表情包与所有表情（各自按 sort、id 升序），由服务层组装成嵌套视图。
	ListPacks(ctx context.Context) ([]EmojiPack, []Emoji, error)
	CreatePack(ctx context.Context, name string, sort int) (EmojiPack, error)
	UpdatePack(ctx context.Context, id int64, name string, sort int) (EmojiPack, error)
	DeletePack(ctx context.Context, id int64) (EmojiPack, error)
	CreateEmoji(ctx context.Context, packID int64, code, image string, sort int) (Emoji, error)
	UpdateEmoji(ctx context.Context, id int64, code, image string, sort int) (Emoji, error)
	DeleteEmoji(ctx context.Context, id int64) (Emoji, error)
}

// EmojiService 处理表情包 / 表情的前台下发与后台维护（docs/03 第 5.4 节，docs/06「表情/表情包管理」）。
type EmojiService struct {
	repo EmojiRepo
}

func NewEmojiService(repo EmojiRepo) *EmojiService {
	return &EmojiService{repo: repo}
}

// Front 返回前台表情面板：每个表情包含其表情（按 sort）。空包也下发（后台可能在排版）。
func (s *EmojiService) Front(ctx context.Context) ([]EmojiPackView, error) {
	packs, emojis, err := s.repo.ListPacks(ctx)
	if err != nil {
		return nil, err
	}
	byPack := make(map[int64][]EmojiView, len(packs))
	for _, e := range emojis {
		byPack[e.PackID] = append(byPack[e.PackID], EmojiView{Code: e.Code, Image: e.Image})
	}
	out := make([]EmojiPackView, 0, len(packs))
	for _, p := range packs {
		items := byPack[p.ID]
		if items == nil {
			items = []EmojiView{}
		}
		out = append(out, EmojiPackView{ID: p.ID, Name: p.Name, Emojis: items})
	}
	return out, nil
}

// PackWithEmojis 是后台列表的一项：表情包 + 其表情。
type PackWithEmojis struct {
	Pack   EmojiPack
	Emojis []Emoji
}

// List 返回后台用的全部表情包（含各自的表情），按 sort。
func (s *EmojiService) List(ctx context.Context) ([]PackWithEmojis, error) {
	packs, emojis, err := s.repo.ListPacks(ctx)
	if err != nil {
		return nil, err
	}
	byPack := make(map[int64][]Emoji, len(packs))
	for _, e := range emojis {
		byPack[e.PackID] = append(byPack[e.PackID], e)
	}
	out := make([]PackWithEmojis, len(packs))
	for i, p := range packs {
		items := byPack[p.ID]
		if items == nil {
			items = []Emoji{}
		}
		out[i] = PackWithEmojis{Pack: p, Emojis: items}
	}
	return out, nil
}

// CreatePack 新建表情包。
func (s *EmojiService) CreatePack(ctx context.Context, name string, sort int) (EmojiPack, error) {
	name, err := cleanPackName(name)
	if err != nil {
		return EmojiPack{}, err
	}
	return s.repo.CreatePack(ctx, name, sort)
}

// UpdatePack 改表情包名与排序。
func (s *EmojiService) UpdatePack(ctx context.Context, id int64, name string, sort int) (EmojiPack, error) {
	name, err := cleanPackName(name)
	if err != nil {
		return EmojiPack{}, err
	}
	return s.repo.UpdatePack(ctx, id, name, sort)
}

// DeletePack 删表情包（级联删其表情），返回被删的包供审计。
func (s *EmojiService) DeletePack(ctx context.Context, id int64) (EmojiPack, error) {
	return s.repo.DeletePack(ctx, id)
}

// CreateEmoji 往某个表情包里加表情。
func (s *EmojiService) CreateEmoji(ctx context.Context, packID int64, code, image string, sort int) (Emoji, error) {
	code, image, err := cleanEmoji(code, image)
	if err != nil {
		return Emoji{}, err
	}
	return s.repo.CreateEmoji(ctx, packID, code, image, sort)
}

// UpdateEmoji 改表情的码 / 图 / 排序。
func (s *EmojiService) UpdateEmoji(ctx context.Context, id int64, code, image string, sort int) (Emoji, error) {
	code, image, err := cleanEmoji(code, image)
	if err != nil {
		return Emoji{}, err
	}
	return s.repo.UpdateEmoji(ctx, id, code, image, sort)
}

// DeleteEmoji 删表情，返回被删的表情供审计。
func (s *EmojiService) DeleteEmoji(ctx context.Context, id int64) (Emoji, error) {
	return s.repo.DeleteEmoji(ctx, id)
}

func cleanPackName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errs.ErrBadRequest.WithMessage("表情包名称不能为空")
	}
	if runeLen(name) > maxEmojiPackNameRunes {
		return "", errs.ErrBadRequest.WithMessage("表情包名称最多 32 个字")
	}
	return name, nil
}

// cleanEmoji 校验并规范化表情码与图片。码须形如 [xxx]（与评论文本里的表情码一致），图片为资源路径或地址。
func cleanEmoji(code, image string) (string, string, error) {
	code = strings.TrimSpace(code)
	image = strings.TrimSpace(image)
	if code == "" {
		return "", "", errs.ErrBadRequest.WithMessage("表情码不能为空")
	}
	if !strings.HasPrefix(code, "[") || !strings.HasSuffix(code, "]") || runeLen(code) < 3 {
		return "", "", errs.ErrBadRequest.WithMessage("表情码须形如 [微笑]")
	}
	if runeLen(code) > maxEmojiCodeRunes {
		return "", "", errs.ErrBadRequest.WithMessage("表情码最多 32 个字")
	}
	if image == "" {
		return "", "", errs.ErrBadRequest.WithMessage("表情图片不能为空")
	}
	if runeLen(image) > maxEmojiImageRunes {
		return "", "", errs.ErrBadRequest.WithMessage("表情图片地址过长")
	}
	return code, image, nil
}
