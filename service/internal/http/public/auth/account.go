package auth

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"xsite/service/internal/domain/identity"
	"xsite/service/internal/domain/user"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// 注册按客户端 IP 前缀限流，挡住批量注册刷号（docs/03 第 2.2 节：注册接口单独限流）。
// 登录的失败计数与人机验证挑战随 S2-2。
const (
	registerPerMinute = 10
	// challengeAlgorithm 说明 PoW 解法：求 solution 使 SHA-256(nonce + ":" + solution) 的前 difficulty 个 bit 为 0。
	challengeAlgorithm = "sha256-leading-zero-bits"
)

// UserService 是注册用户服务（domain/user.Service）。
type UserService interface {
	Register(ctx context.Context, in user.RegisterInput, device identity.IssueInput) (user.AuthResult, error)
	Login(ctx context.Context, in user.LoginInput, device identity.IssueInput) (user.AuthResult, error)
	IssueChallenge(ctx context.Context) (user.Challenge, error)
}

type registerRequest struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	Nickname      string `json:"nickname"`
	AckNoRecovery bool   `json:"ack_no_recovery"`
}

type loginRequest struct {
	Username  string           `json:"username"`
	Password  string           `json:"password"`
	Challenge *challengeAnswer `json:"challenge"`
}

type challengeAnswer struct {
	Nonce    string `json:"nonce"`
	Solution string `json:"solution"`
}

type challengeResponse struct {
	Nonce      string `json:"nonce"`
	Difficulty int    `json:"difficulty"`
	ExpiresIn  int64  `json:"expires_in"`
	Algorithm  string `json:"algorithm"`
}

type userBrief struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Status   string `json:"status"`
}

type authResponse struct {
	User         userBrief `json:"user"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int64     `json:"expires_in"`
	Role         string    `json:"role"`
}

// Register 是 POST /api/v1/auth/register：注册新用户并签发令牌（请求体必填各字段，校验在领域层）。
func (h *Handlers) Register(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	var req registerRequest
	if err := bindOptionalJSON(c, &req); err != nil {
		respond.Error(c, err)
		return
	}
	if wait := h.overRegisterLimit(c); wait > 0 {
		respond.Error(c, errs.WithRetryAfter(errs.ErrRateLimited, wait))
		return
	}
	res, err := h.Users.Register(c.Request.Context(), user.RegisterInput{
		Username: req.Username, Password: req.Password, Nickname: req.Nickname, AckNoRecovery: req.AckNoRecovery,
	}, h.deviceInput(c))
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, toAuthResponse(res))
}

// Login 是 POST /api/v1/auth/login：用户名 + 密码登录并签发令牌。失败统一提示，不区分用户名是否存在。
func (h *Handlers) Login(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	var req loginRequest
	if err := bindOptionalJSON(c, &req); err != nil {
		respond.Error(c, err)
		return
	}
	in := user.LoginInput{Username: req.Username, Password: req.Password}
	if req.Challenge != nil {
		in.Challenge = &user.ChallengeAnswer{Nonce: req.Challenge.Nonce, Solution: req.Challenge.Solution}
	}
	res, err := h.Users.Login(c.Request.Context(), in, h.deviceInput(c))
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, toAuthResponse(res))
}

// Challenge 是 GET /api/v1/auth/challenge：下发人机验证挑战，登录失败过多时客户端据此求解后重试登录。
func (h *Handlers) Challenge(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	ch, err := h.Users.IssueChallenge(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, challengeResponse{
		Nonce: ch.Nonce, Difficulty: ch.Difficulty, ExpiresIn: ch.ExpiresIn, Algorithm: challengeAlgorithm,
	})
}

// deviceInput 从请求算出签发会话所需的平台与「我的设备」展示信息；平台取 X-Platform（非法时领域层回落 web）。
func (h *Handlers) deviceInput(c *gin.Context) identity.IssueInput {
	return identity.IssueInput{
		Platform: c.GetHeader("X-Platform"),
		UADigest: uaDigest(c), IPPrefix: ipPrefix(c.ClientIP()),
	}
}

// overRegisterLimit 按自然分钟的固定窗口限制注册（按 IP 前缀）；计数器故障时放行。
func (h *Handlers) overRegisterLimit(c *gin.Context) time.Duration {
	if h.RegisterLimiter == nil {
		return 0
	}
	now := time.Now()
	key := "ip:" + ipPrefix(c.ClientIP()) + ":" + strconv.FormatInt(now.Unix()/60, 10)
	n, err := h.RegisterLimiter.Add(c.Request.Context(), key, 1, limitWindow)
	if err != nil || n <= registerPerMinute {
		return 0
	}
	return time.Duration(60-now.Unix()%60) * time.Second
}

func toAuthResponse(res user.AuthResult) authResponse {
	return authResponse{
		User: userBrief{
			ID: res.User.ID, Username: res.User.Username, Nickname: res.User.Nickname,
			Avatar: res.User.Avatar, Status: string(res.User.Status),
		},
		AccessToken: res.Tokens.AccessToken, RefreshToken: res.Tokens.RefreshToken,
		TokenType: tokenType, ExpiresIn: res.Tokens.ExpiresIn, Role: string(res.Tokens.Role),
	}
}
