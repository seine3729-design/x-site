package site

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/errs"
	"xsite/service/internal/htmlsan"
)

// BlockType 是区块类型（docs/06 第 15 节）。
type BlockType string

const (
	BlockCarousel   BlockType = "carousel"    // 轮播大图
	BlockFeature    BlockType = "feature"     // 精选大卡
	BlockPosterGrid BlockType = "poster_grid" // 海报网格
	BlockHScroll    BlockType = "h_scroll"    // 横滑列表
	BlockRank       BlockType = "rank"        // 排行榜
	BlockTopicGroup BlockType = "topic_group" // 专题组
	BlockTagCloud   BlockType = "tag_cloud"   // 标签云
	BlockAd         BlockType = "ad"          // 广告位
	BlockAnnounce   BlockType = "announce"    // 公告条
	BlockCustomHTML BlockType = "custom_html" // 自定义 HTML（要 blocks.custom_html 权限）
)

// BlockTypes 是全部区块类型。
var BlockTypes = []BlockType{
	BlockCarousel, BlockFeature, BlockPosterGrid, BlockHScroll, BlockRank,
	BlockTopicGroup, BlockTagCloud, BlockAd, BlockAnnounce, BlockCustomHTML,
}

// dramaBlockTypes 是需要选剧来源的区块类型。
var dramaBlockTypes = []BlockType{BlockCarousel, BlockFeature, BlockPosterGrid, BlockHScroll}

// IsDramaBlock 表示该类型要配剧的来源。
func IsDramaBlock(t BlockType) bool { return slices.Contains(dramaBlockTypes, t) }

// BlockSourceType 是剧类区块的来源方式。
type BlockSourceType string

const (
	SourceManual BlockSourceType = "manual" // 手选剧集编号
	SourceTopic  BlockSourceType = "topic"  // 用某个专题
	SourceAuto   BlockSourceType = "auto"   // 按规则自动选
)

// BlockSourceTypes 是全部来源方式。
var BlockSourceTypes = []BlockSourceType{SourceManual, SourceTopic, SourceAuto}

// 区块可用的排序与上新窗口（与自动专题同一套规则，docs/02 第 2.5 节）。
var (
	blockSorts   = []content.DramaSort{content.SortLatest, content.SortHot, content.SortRecommend}
	blockWindows = []string{"all", "7d", "14d", "30d", "90d"}
)

// AnnounceKinds 是公告条区块可选的公告方式（站内信不在前台显示）。
var AnnounceKinds = []string{"topbar", "popup"}

// RankBoards 是排行榜区块可选的榜单，AdSlots 是广告位区块可选的位置。
// 都用字符串写死，避免站点编排域反向依赖运营域；与 domain/ops 的同名变量一致，block_test.go 里有一致性检查。
var (
	RankBoards = []string{"drama_week", "drama_month"}
	AdSlots    = []string{
		"global_popup", "home_top", "home_mid", "home_bottom", "footer_banner", "list_feed",
		"detail_banner", "preroll", "pause", "immersive_feed", "text_link", "app_download",
	}
)

// withLimit 返回把条数改成 limit 的规则副本（专题的规则里存的是专题自己的条数）。
func withLimit(r content.TopicRule, limit int) content.TopicRule {
	r.Limit = limit
	return r
}

const (
	maxBlockTitleRunes = 50
	maxBlockSubRunes   = 100
	maxBlockItems      = 60
	minBlockItems      = 1
	maxBlockCols       = 8
	minBlockCols       = 2
	maxBlockDramaIDs   = 60
	maxBlockTopicIDs   = 20
	maxBlockTagIDs     = 50
	maxBlockTermCodes  = 10
	maxBlockHTMLRunes  = 20_000
	maxBlocksPerPage   = 50
	homePage           = "home"
	channelPagePrefix  = "channel:"
)

// BlockSource 是区块的来源与类型参数（存 block.source_json，只保留该类型用得到的字段）。
type BlockSource struct {
	// 剧类：手选
	DramaIDs []int64 `json:"drama_ids,omitempty"`
	// 剧类：用某个专题
	TopicID int64 `json:"topic_id,omitempty"`
	// 剧类：自动选（与自动专题同一套规则）
	TermCodes []string          `json:"term_codes,omitempty"`
	TagID     *int64            `json:"tag_id,omitempty"`
	Sort      content.DramaSort `json:"sort,omitempty"`
	Window    string            `json:"window,omitempty"`
	// 排行榜
	RankBoard string `json:"rank_board,omitempty"`
	// 广告位
	AdSlot string `json:"ad_slot,omitempty"`
	// 公告条
	AnnounceKind string `json:"announce_kind,omitempty"`
	// 专题组（不填取「列在专题列表页」的）
	TopicIDs []int64 `json:"topic_ids,omitempty"`
	// 标签云（不填取热门标签）
	TagIDs []int64 `json:"tag_ids,omitempty"`
}

// rule 把自动来源的参数转成选剧规则。
func (s BlockSource) rule(limit int) content.TopicRule {
	return content.TopicRule{TermCodes: s.TermCodes, TagID: s.TagID, Sort: s.Sort, Window: s.Window, Limit: limit}
}

// Block 是一个页面区块。
type Block struct {
	ID          int64
	Page        string // home 或 channel:{频道 slug}
	Type        BlockType
	Title       string
	Subtitle    string
	ItemLimit   int
	Status      MenuStatus
	MoreLink    string
	ColsDesktop int
	Device      MenuDevice
	SourceType  BlockSourceType
	Source      BlockSource
	CustomHTML  string
	Sort        int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// BlockInput 是区块的新建与整体保存。
type BlockInput struct {
	Page        string
	Type        BlockType
	Title       string
	Subtitle    string
	ItemLimit   int
	Status      MenuStatus
	MoreLink    string
	ColsDesktop int
	Device      MenuDevice
	SourceType  BlockSourceType
	Source      BlockSource
	CustomHTML  string
}

// BlockPage 是后台「选页面」的一项。
type BlockPage struct {
	Value string // home 或 channel:{slug}
	Label string // 首页 / 频道名
}

// BlockTag 是标签云区块的一项。
type BlockTag struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// BlockTopicRef 是专题来源要用到的专题（含自动专题的规则）。
type BlockTopicRef struct {
	ID         int64
	Title      string
	Slug       string
	Summary    string
	SourceType content.SourceType
	Rule       content.TopicRule
}

// PublicBlock 是下发给前台的一个区块。按类型只带该类型的数据。
type PublicBlock struct {
	ID          int64      `json:"id"`
	Type        BlockType  `json:"type"`
	Title       string     `json:"title"`
	Subtitle    string     `json:"subtitle"`
	MoreLink    string     `json:"more_link"`
	ColsDesktop int        `json:"cols_desktop"`
	Device      MenuDevice `json:"device"`
	// 剧类与排行榜区块的剧卡片
	Dramas []content.DramaCard `json:"dramas,omitempty"`
	// 排行榜区块的榜单名
	RankBoard string `json:"rank_board,omitempty"`
	// 专题组区块
	Topics []content.TopicCard `json:"topics,omitempty"`
	// 标签云区块
	Tags []BlockTag `json:"tags,omitempty"`
	// 广告位区块：素材由前台另调 /api/v1/ads（按设备频控，不能进共享缓存）
	AdSlot string `json:"ad_slot,omitempty"`
	// 公告条区块：内容由前台另调 /api/v1/announcements
	AnnounceKind string `json:"announce_kind,omitempty"`
	// 自定义 HTML 区块（已清洗，前台在沙箱 iframe 中渲染）
	HTML string `json:"html,omitempty"`
}

// PublicHome 是一个页面的区块编排（GET /api/v1/home?page=）。
type PublicHome struct {
	Page   string        `json:"page"`
	Blocks []PublicBlock `json:"blocks"`
}

// BlockRepo 读写区块与区块要显示的内容。
type BlockRepo interface {
	ListBlocks(ctx context.Context, page string) ([]Block, error)
	GetBlock(ctx context.Context, id int64) (Block, error)
	CreateBlock(ctx context.Context, in BlockInput) (Block, error)
	UpdateBlock(ctx context.Context, id int64, in BlockInput) (Block, error)
	DeleteBlock(ctx context.Context, id int64) (Block, error)
	BlockIDsAt(ctx context.Context, page string) ([]int64, error)
	SetBlockOrder(ctx context.Context, page string, ids []int64) error
	// BlockPages 返回可编排的页面：首页与启用的频道页。
	BlockPages(ctx context.Context) ([]BlockPage, error)

	// MissingDramas 返回 ids 中不存在或在回收站的剧。
	MissingDramas(ctx context.Context, ids []int64) ([]int64, error)
	// BlockDramaCards 按 ids 的顺序返回前台可见的剧卡片。
	BlockDramaCards(ctx context.Context, ids []int64) ([]content.DramaCard, error)
	// BlockTopic 返回已发布的专题（含自动专题规则）；查不到时 errs.ErrNotFound。
	BlockTopic(ctx context.Context, id int64) (BlockTopicRef, error)
	// BlockTopicDramas 返回手选专题里前台可见的剧。
	BlockTopicDramas(ctx context.Context, topicID int64, limit int) ([]content.DramaCard, error)
	BlockTopicCards(ctx context.Context, ids []int64) ([]content.TopicCard, error)
	BlockIndexTopics(ctx context.Context, limit int) ([]content.TopicCard, error)
	BlockTagsByIDs(ctx context.Context, ids []int64) ([]BlockTag, error)
	BlockTopTags(ctx context.Context, limit int) ([]BlockTag, error)
	ExistingTopicIDs(ctx context.Context, ids []int64) ([]int64, error)
	ExistingTagIDs(ctx context.Context, ids []int64) ([]int64, error)
	// ExistingTermCodes 返回 codes 中存在的维度取值编号（自动来源的校验）。
	ExistingTermCodes(ctx context.Context, codes []string) ([]string, error)
	// MenuTargets 复用菜单的目标校验（频道页用 LinkChannel）。
	MenuTargets(ctx context.Context, targets []MenuTarget) (map[MenuTarget]bool, error)
}

// DramaPicker 按规则选剧（实现见 domain/content.PublicService.PickDramas）。
type DramaPicker interface {
	PickDramas(ctx context.Context, rule content.TopicRule) ([]content.DramaCard, error)
}

// RankBoardReader 读榜单前 limit 名的剧卡片（实现见 app 层对 domain/ops.RankService 的适配）。
type RankBoardReader interface {
	RankDramas(ctx context.Context, board string, limit int) ([]content.DramaCard, error)
}

// ErrBlockCustomHTMLForbidden 是没有 blocks.custom_html 权限时新建或修改自定义 HTML 区块的错误。
var ErrBlockCustomHTMLForbidden = errs.ErrForbidden.WithMessage("自定义 HTML 区块需要「自定义 HTML」权限（默认只有超级管理员）")

// 前台区块编排的缓存：后台增删改后立即删除，TTL 兜底的是剧集上下架等别的模块的改动。
const blockCacheTTL = time.Minute

func blockCacheKey(page string) string { return "site:home:" + page }

// BlockService 是页面区块的后台管理与前台下发。
type BlockService struct {
	repo   BlockRepo
	cache  Cache
	picker DramaPicker
	ranks  RankBoardReader
}

// NewBlockService 创建区块服务。
func NewBlockService(repo BlockRepo, cache Cache, picker DramaPicker, ranks RankBoardReader) *BlockService {
	return &BlockService{repo: repo, cache: cache, picker: picker, ranks: ranks}
}

// Pages 返回可编排的页面（首页 + 启用的频道页）。
func (s *BlockService) Pages(ctx context.Context) ([]BlockPage, error) {
	return s.repo.BlockPages(ctx)
}

// List 返回某个页面的全部区块（后台，按顺序）。
func (s *BlockService) List(ctx context.Context, page string) ([]Block, error) {
	page, err := s.cleanPageRef(ctx, page)
	if err != nil {
		return nil, err
	}
	return s.repo.ListBlocks(ctx, page)
}

// Create 新建区块（放在该页面的末尾）。customHTML 表示当前管理员有 blocks.custom_html 权限。
func (s *BlockService) Create(ctx context.Context, in BlockInput, customHTML bool) (Block, error) {
	in, err := s.cleanBlock(ctx, in, customHTML)
	if err != nil {
		return Block{}, err
	}
	existing, err := s.repo.BlockIDsAt(ctx, in.Page)
	if err != nil {
		return Block{}, err
	}
	if len(existing) >= maxBlocksPerPage {
		return Block{}, badRequest("单个页面最多 %d 个区块", maxBlocksPerPage)
	}
	b, err := s.repo.CreateBlock(ctx, in)
	if err == nil {
		s.invalidate(ctx, b.Page)
	}
	return b, err
}

// Update 整体保存区块，返回修改前后的记录。原本是自定义 HTML 的区块也要权限才能改。
func (s *BlockService) Update(ctx context.Context, id int64, in BlockInput, customHTML bool) (before, after Block, err error) {
	if before, err = s.repo.GetBlock(ctx, id); err != nil {
		return
	}
	if before.Type == BlockCustomHTML && !customHTML {
		return before, after, ErrBlockCustomHTMLForbidden
	}
	if in, err = s.cleanBlock(ctx, in, customHTML); err != nil {
		return
	}
	if after, err = s.repo.UpdateBlock(ctx, id, in); err == nil {
		s.invalidate(ctx, before.Page, after.Page)
	}
	return
}

// Delete 删除区块（不可恢复），返回删除前的记录。删除只要 blocks.edit。
func (s *BlockService) Delete(ctx context.Context, id int64) (Block, error) {
	b, err := s.repo.DeleteBlock(ctx, id)
	if err == nil {
		s.invalidate(ctx, b.Page)
	}
	return b, err
}

// Reorder 按 ids 的顺序重排某个页面。ids 必须恰好是该页面现有的全部区块，否则 409。
func (s *BlockService) Reorder(ctx context.Context, page string, ids []int64) error {
	page, err := s.cleanPageRef(ctx, page)
	if err != nil {
		return err
	}
	current, err := s.repo.BlockIDsAt(ctx, page)
	if err != nil {
		return err
	}
	if !sameSet(current, ids) {
		return errs.ErrConflict.WithMessage("区块已被改动，请刷新后重新排序")
	}
	if err := s.repo.SetBlockOrder(ctx, page, ids); err != nil {
		return err
	}
	s.invalidate(ctx, page)
	return nil
}

// Home 返回某个页面的区块编排（前台）：只含启用的区块，每个区块带好数据。
// 单个区块取数失败只记日志并跳过，不影响整页（docs/04 第 1 节）。
func (s *BlockService) Home(ctx context.Context, page string) (PublicHome, error) {
	page, err := s.cleanPageRef(ctx, page)
	if err != nil {
		return PublicHome{}, err
	}
	var out PublicHome
	err = s.cache.GetOrLoadJSON(ctx, blockCacheKey(page), blockCacheTTL, &out, func(ctx context.Context) (any, error) {
		blocks, err := s.repo.ListBlocks(ctx, page)
		if err != nil {
			return nil, err
		}
		list := make([]PublicBlock, 0, len(blocks))
		for _, b := range blocks {
			if b.Status != MenuActive {
				continue
			}
			pb, ok := s.buildBlock(ctx, b)
			if !ok {
				continue
			}
			list = append(list, pb)
		}
		return PublicHome{Page: page, Blocks: list}, nil
	})
	if err != nil {
		return PublicHome{}, err
	}
	if out.Blocks == nil {
		out.Blocks = []PublicBlock{}
	}
	return out, nil
}

// buildBlock 组装一个区块的数据。取数失败或没有内容时返回 false（该区块不下发）。
func (s *BlockService) buildBlock(ctx context.Context, b Block) (PublicBlock, bool) {
	pb := PublicBlock{
		ID: b.ID, Type: b.Type, Title: b.Title, Subtitle: b.Subtitle, MoreLink: b.MoreLink,
		ColsDesktop: b.ColsDesktop, Device: b.Device,
	}
	limit := b.ItemLimit
	var err error
	switch {
	case IsDramaBlock(b.Type):
		pb.Dramas, err = s.blockDramas(ctx, b)
	case b.Type == BlockRank:
		pb.RankBoard = b.Source.RankBoard
		pb.Dramas, err = s.ranks.RankDramas(ctx, b.Source.RankBoard, limit)
	case b.Type == BlockTopicGroup:
		if len(b.Source.TopicIDs) > 0 {
			pb.Topics, err = s.repo.BlockTopicCards(ctx, b.Source.TopicIDs)
		} else {
			pb.Topics, err = s.repo.BlockIndexTopics(ctx, limit)
		}
		pb.Topics = truncate(pb.Topics, limit)
	case b.Type == BlockTagCloud:
		if len(b.Source.TagIDs) > 0 {
			pb.Tags, err = s.repo.BlockTagsByIDs(ctx, b.Source.TagIDs)
		} else {
			pb.Tags, err = s.repo.BlockTopTags(ctx, limit)
		}
		pb.Tags = truncate(pb.Tags, limit)
	case b.Type == BlockAd:
		pb.AdSlot = b.Source.AdSlot
	case b.Type == BlockAnnounce:
		pb.AnnounceKind = b.Source.AnnounceKind
	case b.Type == BlockCustomHTML:
		pb.HTML = b.CustomHTML
	}
	if err != nil {
		slog.ErrorContext(ctx, "区块取数失败，本次不下发该区块", "block_id", b.ID, "type", b.Type, "err", err)
		return PublicBlock{}, false
	}
	// 列表类区块没有内容时不下发，前台少一个空块（广告位、公告条、自定义 HTML 没有列表）
	if len(pb.Dramas) == 0 && len(pb.Topics) == 0 && len(pb.Tags) == 0 &&
		pb.AdSlot == "" && pb.AnnounceKind == "" && pb.HTML == "" {
		return PublicBlock{}, false
	}
	return pb, true
}

// blockDramas 按来源取剧类区块的剧。
func (s *BlockService) blockDramas(ctx context.Context, b Block) ([]content.DramaCard, error) {
	limit := b.ItemLimit
	switch b.SourceType {
	case SourceManual:
		cards, err := s.repo.BlockDramaCards(ctx, b.Source.DramaIDs)
		return truncate(cards, limit), err
	case SourceTopic:
		t, err := s.repo.BlockTopic(ctx, b.Source.TopicID)
		if errs.From(err).Code == errs.ErrNotFound.Code {
			return nil, nil // 专题被删除或改回草稿：这个区块暂时没有内容
		}
		if err != nil {
			return nil, err
		}
		if t.SourceType == content.SourceAuto {
			return s.picker.PickDramas(ctx, withLimit(t.Rule, limit))
		}
		return s.repo.BlockTopicDramas(ctx, t.ID, limit)
	default:
		return s.picker.PickDramas(ctx, b.Source.rule(limit))
	}
}

// invalidate 清掉受影响页面的区块缓存。失败只记日志：最多 blockCacheTTL 后自然过期。
func (s *BlockService) invalidate(ctx context.Context, pages ...string) {
	for _, page := range slices.Compact(slices.Sorted(slices.Values(pages))) {
		if page == "" {
			continue
		}
		if err := s.cache.Delete(ctx, blockCacheKey(page)); err != nil {
			slog.WarnContext(ctx, "清除区块缓存失败，稍后随 TTL 过期", "page", page, "err", err)
		}
	}
}

// cleanPageRef 校验页面：home 或 channel:{启用的频道取值 slug}。
func (s *BlockService) cleanPageRef(ctx context.Context, page string) (string, error) {
	page = strings.TrimSpace(page)
	if page == "" || page == homePage {
		return homePage, nil
	}
	slug, ok := strings.CutPrefix(page, channelPagePrefix)
	if !ok {
		return "", errs.ErrNotFound
	}
	slug = strings.ToLower(slug)
	if !content.ValidSlug(slug) {
		return "", errs.ErrNotFound
	}
	target := MenuTarget{Kind: LinkChannel, Slug: slug}
	state, err := s.repo.MenuTargets(ctx, []MenuTarget{target})
	if err != nil {
		return "", err
	}
	// 与菜单不同：这里要求频道取值当前是启用的（停用的取值没有前台频道页，也不出现在「选页面」下拉里）
	if !state[target] {
		return "", errs.ErrNotFound
	}
	return channelPagePrefix + slug, nil
}

// cleanBlock 校验并规范化区块输入：按类型只保留用得到的参数。
func (s *BlockService) cleanBlock(ctx context.Context, in BlockInput, customHTML bool) (BlockInput, error) {
	var err error
	if in.Page, err = s.cleanPageRef(ctx, in.Page); err != nil {
		return in, badRequest("页面只能是首页或某个启用的频道页")
	}
	if !slices.Contains(BlockTypes, in.Type) {
		return in, badRequest("请选择区块类型")
	}
	if in.Title, err = cleanText("标题", in.Title, maxBlockTitleRunes, false); err != nil {
		return in, err
	}
	if in.Subtitle, err = cleanText("副标题", in.Subtitle, maxBlockSubRunes, false); err != nil {
		return in, err
	}
	if in.ItemLimit == 0 {
		in.ItemLimit = 24
	}
	if in.ItemLimit < minBlockItems || in.ItemLimit > maxBlockItems {
		return in, badRequest("数量为 %d–%d", minBlockItems, maxBlockItems)
	}
	if in.ColsDesktop == 0 {
		in.ColsDesktop = 6
	}
	if in.ColsDesktop < minBlockCols || in.ColsDesktop > maxBlockCols {
		return in, badRequest("电脑每行个数为 %d–%d", minBlockCols, maxBlockCols)
	}
	if in.MoreLink, err = cleanLink("查看全部链接", in.MoreLink); err != nil {
		return in, err
	}
	switch in.Device {
	case "":
		in.Device = DeviceAll
	case DeviceAll, DeviceMobile, DeviceDesktop:
	default:
		return in, badRequest("设备只能是全部、手机或电脑")
	}
	switch in.Status {
	case "":
		in.Status = MenuActive
	case MenuActive, MenuInactive:
	default:
		return in, badRequest("状态只能是启用或停用")
	}
	if in.Source, in.SourceType, err = s.cleanSource(ctx, in.Type, in.SourceType, in.Source); err != nil {
		return in, err
	}
	if in.Type == BlockCustomHTML {
		if !customHTML {
			return in, ErrBlockCustomHTMLForbidden
		}
		if in.CustomHTML, err = sanitizeBlockHTML(in.CustomHTML); err != nil {
			return in, err
		}
	} else {
		in.CustomHTML = ""
	}
	return in, nil
}

// cleanSource 按类型校验来源参数，并清掉该类型用不到的字段。
func (s *BlockService) cleanSource(ctx context.Context, t BlockType, st BlockSourceType, src BlockSource) (BlockSource, BlockSourceType, error) {
	out := BlockSource{}
	if !IsDramaBlock(t) {
		// 非剧类区块不需要来源方式；统一记成 auto，便于库里有个确定值
		st = SourceAuto
	}
	switch {
	case IsDramaBlock(t):
		switch st {
		case SourceManual:
			ids, err := cleanIDs("剧集编号", src.DramaIDs, maxBlockDramaIDs, true)
			if err != nil {
				return out, st, err
			}
			missing, err := s.repo.MissingDramas(ctx, ids)
			if err != nil {
				return out, st, err
			}
			if len(missing) > 0 {
				return out, st, badRequest("剧集 %s 不存在或在回收站", joinIDs(missing))
			}
			out.DramaIDs = ids
		case SourceTopic:
			if src.TopicID <= 0 {
				return out, st, badRequest("请选择专题")
			}
			found, err := s.repo.ExistingTopicIDs(ctx, []int64{src.TopicID})
			if err != nil {
				return out, st, err
			}
			if len(found) == 0 {
				return out, st, badRequest("专题 #%d 不存在", src.TopicID)
			}
			out.TopicID = src.TopicID
		case SourceAuto, "":
			st = SourceAuto
			codes, err := cleanStrings("维度取值编号", src.TermCodes, maxBlockTermCodes, 32)
			if err != nil {
				return out, st, err
			}
			if len(codes) > 0 {
				existing, err := s.repo.ExistingTermCodes(ctx, codes)
				if err != nil {
					return out, st, err
				}
				for _, c := range codes {
					if !slices.Contains(existing, c) {
						return out, st, badRequest("维度取值编号「%s」不存在", c)
					}
				}
			}
			out.TermCodes = codes
			if src.TagID != nil {
				if *src.TagID <= 0 {
					return out, st, badRequest("标签无效")
				}
				found, err := s.repo.ExistingTagIDs(ctx, []int64{*src.TagID})
				if err != nil {
					return out, st, err
				}
				if len(found) == 0 {
					return out, st, badRequest("标签 #%d 不存在", *src.TagID)
				}
				out.TagID = src.TagID
			}
			if src.Sort == "" {
				src.Sort = content.SortLatest
			}
			if !slices.Contains(blockSorts, src.Sort) {
				return out, st, badRequest("排序只能是最新、最热或推荐")
			}
			if src.Window == "" {
				src.Window = "all"
			}
			if !slices.Contains(blockWindows, src.Window) {
				return out, st, badRequest("上新时间只能是不限、7d、14d、30d 或 90d")
			}
			out.Sort, out.Window = src.Sort, src.Window
		default:
			return out, st, badRequest("剧来源只能是手选、用专题或自动选")
		}
	case t == BlockRank:
		if !slices.Contains(RankBoards, src.RankBoard) {
			return out, st, badRequest("请选择榜单")
		}
		out.RankBoard = src.RankBoard
	case t == BlockAd:
		if !slices.Contains(AdSlots, src.AdSlot) {
			return out, st, badRequest("请选择广告位置")
		}
		out.AdSlot = src.AdSlot
	case t == BlockAnnounce:
		if src.AnnounceKind == "" {
			src.AnnounceKind = "topbar"
		}
		if !slices.Contains(AnnounceKinds, src.AnnounceKind) {
			return out, st, badRequest("公告方式只能是顶部滚动条或弹窗")
		}
		out.AnnounceKind = src.AnnounceKind
	case t == BlockTopicGroup:
		ids, err := cleanIDs("专题编号", src.TopicIDs, maxBlockTopicIDs, false)
		if err != nil {
			return out, st, err
		}
		if len(ids) > 0 {
			found, err := s.repo.ExistingTopicIDs(ctx, ids)
			if err != nil {
				return out, st, err
			}
			if len(found) != len(ids) {
				return out, st, badRequest("有专题编号不存在，请确认")
			}
		}
		out.TopicIDs = ids
	case t == BlockTagCloud:
		ids, err := cleanIDs("标签编号", src.TagIDs, maxBlockTagIDs, false)
		if err != nil {
			return out, st, err
		}
		if len(ids) > 0 {
			found, err := s.repo.ExistingTagIDs(ctx, ids)
			if err != nil {
				return out, st, err
			}
			if len(found) != len(ids) {
				return out, st, badRequest("有标签编号不存在，请确认")
			}
		}
		out.TagIDs = ids
	}
	return out, st, nil
}

// sanitizeBlockHTML 按白名单清洗自定义 HTML；清洗后什么都不剩时拒绝保存。
func sanitizeBlockHTML(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", badRequest("请填写自定义 HTML")
	}
	if utf8.RuneCountInString(raw) > maxBlockHTMLRunes {
		return "", badRequest("自定义 HTML 不能超过 %d 个字符", maxBlockHTMLRunes)
	}
	out := htmlsan.Sanitize(raw)
	if out == "" {
		return "", badRequest("自定义 HTML 清洗后为空：只允许常见排版标签、链接与图片，脚本、样式表、iframe 与事件属性会被去掉")
	}
	return out, nil
}

// JSON 把来源参数序列化进 source_json。
func (s BlockSource) JSON() ([]byte, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("序列化区块来源: %w", err)
	}
	return b, nil
}

// ParseBlockSource 解析 source_json。
func ParseBlockSource(raw []byte) (BlockSource, error) {
	var s BlockSource
	if len(raw) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return BlockSource{}, fmt.Errorf("解析区块来源: %w", err)
	}
	return s, nil
}

// cleanIDs 校验一组编号：去重保序、必须为正数、不超过 max；required 为真时不能为空。
func cleanIDs(label string, ids []int64, max int, required bool) ([]int64, error) {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, badRequest("%s无效：%d", label, id)
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	if required && len(out) == 0 {
		return nil, badRequest("请至少选一个%s", label)
	}
	if len(out) > max {
		return nil, badRequest("%s最多 %d 个", label, max)
	}
	return out, nil
}

// cleanStrings 校验一组短字符串（去空白、去重保序、限制条数与长度）。
func cleanStrings(label string, values []string, max, maxLen int) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if utf8.RuneCountInString(v) > maxLen {
			return nil, badRequest("%s不能超过 %d 个字符", label, maxLen)
		}
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	if len(out) > max {
		return nil, badRequest("%s最多 %d 个", label, max)
	}
	return out, nil
}

func joinIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("#%d", id)
	}
	return strings.Join(parts, "、")
}

// truncate 截断到前 n 项。
func truncate[T any](list []T, n int) []T {
	if n > 0 && len(list) > n {
		return list[:n]
	}
	return list
}
