package community

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/http/respond"
)

// emojiMaxAge 是表情包的共享缓存时长：1 天（docs/03 第 5.4 节）。
const emojiMaxAge = 86400

// EmojiService 是表情服务（domain/moderation.EmojiService）的前台部分。
type EmojiService interface {
	Front(ctx context.Context) ([]dom.EmojiPackView, error)
}

// ListEmojis 是 GET /api/v1/emojis：下发全部表情包（含表情），公开读。
// 按内容派生 ETag、强缓存 1 天；表情变更后内容哈希变化即失效（比绑定 site.version 更即时）。
func (h *Handlers) ListEmojis(c *gin.Context) {
	packs, err := h.Emojis.Front(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	payload := gin.H{"packs": packs}
	b, err := json.Marshal(payload)
	if err != nil {
		respond.Error(c, err)
		return
	}
	sum := sha256.Sum256(b)
	c.Header("Cache-Control", fmt.Sprintf("public, max-age=0, s-maxage=%d, stale-while-revalidate=60", emojiMaxAge))
	if respond.NotModified(c, fmt.Sprintf(`W/"emojis-%s"`, hex.EncodeToString(sum[:8]))) {
		return
	}
	respond.OK(c, json.RawMessage(b))
}
