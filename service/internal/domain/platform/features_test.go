package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeFeatureRepo struct {
	features json.RawMessage
	ageGate  json.RawMessage
	err      error
	setErr   error
}

func (r *fakeFeatureRepo) GetFeatureConfig(context.Context) (json.RawMessage, json.RawMessage, error) {
	if r.err != nil {
		return nil, nil, r.err
	}
	return r.features, r.ageGate, nil
}

func (r *fakeFeatureRepo) SetFeatureFlags(_ context.Context, features json.RawMessage) error {
	if r.setErr != nil {
		return r.setErr
	}
	r.features = features
	return nil
}

func (r *fakeFeatureRepo) SetAgeGate(_ context.Context, ageGate json.RawMessage) error {
	if r.setErr != nil {
		return r.setErr
	}
	r.ageGate = ageGate
	return nil
}

func newFeatureSvc(repo *fakeFeatureRepo, site *fakeSite) *FeatureService {
	return NewFeatureService(repo, site)
}

// 每个开关都要有名称与「关闭影响」说明：docs/06 第 36 节要求「每项附关闭影响说明」。
func TestFeatures_AllFlagsDescribed(t *testing.T) {
	svc := newFeatureSvc(&fakeFeatureRepo{}, &fakeSite{})
	cfg, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Flags) != 8 {
		t.Errorf("开关数 = %d，期望 8（docs/06 第 36 节列了 8 项）", len(cfg.Flags))
	}
	for _, f := range cfg.Flags {
		if f.Title == "" {
			t.Errorf("开关 %s 没有名称", f.Flag)
		}
		if f.Impact == "" {
			t.Errorf("开关 %s 没有关闭影响说明", f.Flag)
		}
	}
}

// 没设置过的开关按打开处理：新增开关不该因为老配置里没有而默认关掉功能。
func TestFeatures_UnsetDefaultsOn(t *testing.T) {
	svc := newFeatureSvc(&fakeFeatureRepo{features: json.RawMessage(`{}`)}, &fakeSite{})
	cfg, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range cfg.Flags {
		if !f.On {
			t.Errorf("未设置的开关 %s 应当视为打开", f.Flag)
		}
	}
}

// 只有明确 false 才是关闭（与 ops.FeatureFlags 的语义一致）。
func TestFeatures_OnlyExplicitFalseIsOff(t *testing.T) {
	repo := &fakeFeatureRepo{features: json.RawMessage(`{"comments":false,"danmaku":true}`)}
	cfg, err := newFeatureSvc(repo, &fakeSite{}).Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byFlag := map[FeatureFlag]bool{}
	for _, f := range cfg.Flags {
		byFlag[f.Flag] = f.On
	}
	if byFlag[FlagComments] {
		t.Error("明确设为 false 的开关应当是关闭")
	}
	if !byFlag[FlagDanmaku] || !byFlag[FlagRanks] {
		t.Error("设为 true 与未设置的开关都应当是打开")
	}
}

// 保存时剔掉已废弃的键：建表默认值里有 articles / actors，这两个模块已经删了（迁移 00010 / 00014）。
func TestFeatures_DropsRetiredFlags(t *testing.T) {
	repo := &fakeFeatureRepo{features: json.RawMessage(`{"comments":true,"articles":true,"actors":false}`)}
	svc := newFeatureSvc(repo, &fakeSite{})
	if _, err := svc.SetFlags(context.Background(), map[string]bool{"comments": false}); err != nil {
		t.Fatal(err)
	}
	var saved map[string]bool
	if err := json.Unmarshal(repo.features, &saved); err != nil {
		t.Fatal(err)
	}
	for _, retired := range []string{"articles", "actors"} {
		if _, ok := saved[retired]; ok {
			t.Errorf("保存后仍留着已废弃的开关 %q", retired)
		}
	}
	if len(saved) != 8 {
		t.Errorf("保存了 %d 个开关，期望只有登记的 8 个", len(saved))
	}
	if saved["comments"] {
		t.Error("comments 应当被关掉")
	}
}

// 未传的开关按打开保存（整体保存语义）。
func TestFeatures_SetFlagsFillsMissing(t *testing.T) {
	repo := &fakeFeatureRepo{}
	svc := newFeatureSvc(repo, &fakeSite{})
	cfg, err := svc.SetFlags(context.Background(), map[string]bool{"ranks": false})
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]bool
	_ = json.Unmarshal(repo.features, &saved)
	if saved["ranks"] {
		t.Error("ranks 应当被关掉")
	}
	if !saved["comments"] || !saved["pwa"] {
		t.Error("未传的开关应当按打开保存")
	}
	// 返回的配置要是保存后的最新状态
	for _, f := range cfg.Flags {
		if f.Flag == FlagRanks && f.On {
			t.Error("返回的配置没有反映刚保存的状态")
		}
	}
}

func TestFeatures_RejectsUnknownFlag(t *testing.T) {
	svc := newFeatureSvc(&fakeFeatureRepo{}, &fakeSite{})
	_, err := svc.SetFlags(context.Background(), map[string]bool{"articles": true})
	if err == nil {
		t.Fatal("已废弃的开关编码应当被拒绝")
	}
	// 报错要列出可选项
	if !strings.Contains(err.Error(), "comments") {
		t.Errorf("报错没列出可选开关：%v", err)
	}
}

// 改开关要让站点版本 +1，否则前台最多等一个缓存周期才生效。
func TestFeatures_BumpsSiteVersion(t *testing.T) {
	site := &fakeSite{version: 3}
	svc := newFeatureSvc(&fakeFeatureRepo{}, site)
	if _, err := svc.SetFlags(context.Background(), map[string]bool{"pwa": false}); err != nil {
		t.Fatal(err)
	}
	if site.bumped != 1 {
		t.Errorf("保存开关后应当让版本 +1，调用了 %d 次", site.bumped)
	}

	site2 := &fakeSite{version: 1}
	svc2 := newFeatureSvc(&fakeFeatureRepo{}, site2)
	gate := AgeGate{Enabled: true, Title: "提示", ConfirmText: "我已成年", DenyText: "离开"}
	if _, err := svc2.SetAgeGate(context.Background(), gate); err != nil {
		t.Fatal(err)
	}
	if site2.bumped != 1 {
		t.Errorf("保存年龄门后应当让版本 +1，调用了 %d 次", site2.bumped)
	}
}

// ---------- 年龄门 ----------

func TestAgeGate_DefaultRememberDays(t *testing.T) {
	cfg, err := newFeatureSvc(&fakeFeatureRepo{}, &fakeSite{}).Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgeGate.RememberDays != ageGateDefaultDay {
		t.Errorf("默认重复间隔 = %d，期望 %d", cfg.AgeGate.RememberDays, ageGateDefaultDay)
	}
	if cfg.AgeGate.Enabled {
		t.Error("年龄门默认应当是关闭的")
	}
}

// 启用时文案不能缺：开着的年龄门挡在所有访客前面，空文案会让人看到无法操作的弹层。
func TestAgeGate_EnabledRequiresText(t *testing.T) {
	cases := []struct {
		name string
		gate AgeGate
		want string
	}{
		{"缺标题", AgeGate{Enabled: true, ConfirmText: "是", DenyText: "否"}, "标题"},
		{"缺确认按钮", AgeGate{Enabled: true, Title: "提示", DenyText: "否"}, "确认按钮文字"},
		{"缺拒绝按钮", AgeGate{Enabled: true, Title: "提示", ConfirmText: "是"}, "拒绝按钮文字"},
		{"只有空格也算缺", AgeGate{Enabled: true, Title: "  ", ConfirmText: "是", DenyText: "否"}, "标题"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := newFeatureSvc(&fakeFeatureRepo{}, &fakeSite{}).SetAgeGate(context.Background(), c.gate)
			if err == nil {
				t.Fatalf("%s 应当被拒绝", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("报错应当指出缺什么（%s）：%v", c.want, err)
			}
		})
	}
}

// 关闭状态下允许文案不全：可以先存草稿再启用。
func TestAgeGate_DisabledAllowsEmptyText(t *testing.T) {
	_, err := newFeatureSvc(&fakeFeatureRepo{}, &fakeSite{}).SetAgeGate(context.Background(), AgeGate{Enabled: false})
	if err != nil {
		t.Errorf("关闭状态下应当允许留空：%v", err)
	}
}

func TestAgeGate_Validation(t *testing.T) {
	base := AgeGate{Title: "提示", ConfirmText: "是", DenyText: "否"}
	cases := []struct {
		name string
		gate AgeGate
		want string
	}{
		{"标题过长", AgeGate{Title: strings.Repeat("字", 61)}, "标题不能超过"},
		{"说明过长", AgeGate{Desc: strings.Repeat("字", 501)}, "说明文字不能超过"},
		{"按钮文字过长", AgeGate{ConfirmText: strings.Repeat("字", 21)}, "确认按钮文字不能超过"},
		{"跳转地址不是网址", AgeGate{DenyURL: "about:blank"}, "完整的 http(s) 网址"},
		{"跳转地址缺主机", AgeGate{DenyURL: "https://"}, "完整的 http(s) 网址"},
		{"跳转地址是 javascript", AgeGate{DenyURL: "javascript:alert(1)"}, "完整的 http(s) 网址"},
		{"重复间隔为负", AgeGate{RememberDays: -1}, "重复间隔"},
		{"重复间隔过大", AgeGate{RememberDays: 366}, "重复间隔"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := newFeatureSvc(&fakeFeatureRepo{}, &fakeSite{}).SetAgeGate(context.Background(), c.gate)
			if err == nil {
				t.Fatalf("%s 应当被拒绝", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("报错不对（期望含 %q）：%v", c.want, err)
			}
		})
	}
	// 合法的完整配置
	ok := base
	ok.Enabled = true
	ok.Desc = "本站内容可能包含成人向情节"
	ok.DenyURL = "https://www.example.com"
	ok.RememberDays = 7
	if _, err := newFeatureSvc(&fakeFeatureRepo{}, &fakeSite{}).SetAgeGate(context.Background(), ok); err != nil {
		t.Errorf("合法配置应当通过：%v", err)
	}
}

// 保存时去掉首尾空白。
func TestAgeGate_TrimsWhitespace(t *testing.T) {
	repo := &fakeFeatureRepo{}
	gate := AgeGate{Enabled: true, Title: "  年龄确认  ", Desc: " 说明 ", ConfirmText: " 是 ", DenyText: " 否 "}
	if _, err := newFeatureSvc(repo, &fakeSite{}).SetAgeGate(context.Background(), gate); err != nil {
		t.Fatal(err)
	}
	var saved AgeGate
	if err := json.Unmarshal(repo.ageGate, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Title != "年龄确认" || saved.Desc != "说明" || saved.ConfirmText != "是" {
		t.Errorf("没有去掉首尾空白: %+v", saved)
	}
}

func TestFeatures_RepoErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := newFeatureSvc(&fakeFeatureRepo{err: errors.New("库挂了")}, &fakeSite{}).Get(ctx); err == nil {
		t.Error("读取出错应当报错")
	}
	svc := newFeatureSvc(&fakeFeatureRepo{setErr: errors.New("写失败")}, &fakeSite{})
	if _, err := svc.SetFlags(ctx, map[string]bool{"pwa": false}); err == nil {
		t.Error("保存开关出错应当报错")
	}
	if _, err := svc.SetAgeGate(ctx, AgeGate{}); err == nil {
		t.Error("保存年龄门出错应当报错")
	}
}

// 库里存的是坏 JSON 时要报错，不能静默当成空配置（那会把所有开关显示成打开）。
func TestFeatures_RejectsCorruptJSON(t *testing.T) {
	svc := newFeatureSvc(&fakeFeatureRepo{features: json.RawMessage(`{not json`)}, &fakeSite{})
	if _, err := svc.Get(context.Background()); err == nil {
		t.Error("坏 JSON 应当报错")
	}
}
