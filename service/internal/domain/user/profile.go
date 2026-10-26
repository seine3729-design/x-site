package user

import "context"

// Profile 是可编辑的用户资料（GET / PATCH /me/profile）。
// *_status 为内容审核状态：S2-3 恒为 ok，昵称 / 简介的屏蔽词与资料审核随 S2-9 接入。
type Profile struct {
	Nickname       string `json:"nickname"`
	Avatar         string `json:"avatar"`
	Bio            string `json:"bio"`
	NicknameStatus string `json:"nickname_status"`
	BioStatus      string `json:"bio_status"`
	AvatarStatus   string `json:"avatar_status"`
	Locale         string `json:"locale,omitempty"`
}

// ProfileUpdate 是资料修改请求（PATCH 语义：nil 字段不改）。
type ProfileUpdate struct {
	Nickname *string
	Avatar   *string
	Bio      *string
}

// ProfileFields 是校验后要写库的字段（nil 不改；仓储层用）。
type ProfileFields struct {
	Nickname *string
	Avatar   *string
	Bio      *string
}

// GetProfile 读当前用户资料。
func (s *Service) GetProfile(ctx context.Context, userID int64) (Profile, error) {
	return s.repo.GetProfile(ctx, userID)
}

// UpdateProfile 修改资料。S2-3 只做基本校验（清洗控制字符 + 长度）；
// 昵称保留词与 30 天改名频率、简介屏蔽词与引流识别、资料审核队列按 docs/13 第 10.4 节归 S2-9。
func (s *Service) UpdateProfile(ctx context.Context, userID int64, upd ProfileUpdate) (Profile, error) {
	var f ProfileFields
	if upd.Nickname != nil {
		nn, err := validateNickname(*upd.Nickname)
		if err != nil {
			return Profile{}, err
		}
		f.Nickname = &nn
	}
	if upd.Bio != nil {
		bio, err := validateBio(*upd.Bio)
		if err != nil {
			return Profile{}, err
		}
		f.Bio = &bio
	}
	if upd.Avatar != nil {
		av, err := validateAvatar(*upd.Avatar)
		if err != nil {
			return Profile{}, err
		}
		f.Avatar = &av
	}
	if f.Nickname == nil && f.Avatar == nil && f.Bio == nil {
		return s.repo.GetProfile(ctx, userID) // 没有可改字段：回读当前资料
	}
	return s.repo.UpdateProfile(ctx, userID, f)
}
