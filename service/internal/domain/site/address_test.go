package site

import (
	"context"
	"strings"
	"testing"

	"xsite/service/internal/errs"
)

// fakeAddressRepo 在内存里存一份防丢地址。
type fakeAddressRepo struct {
	current Addresses
	err     error
}

func (f *fakeAddressRepo) SiteAddresses(context.Context) (Addresses, error) { return f.current, f.err }

func (f *fakeAddressRepo) SetSiteAddresses(_ context.Context, a Addresses) (Addresses, error) {
	if f.err != nil {
		return Addresses{}, f.err
	}
	f.current = a
	return a, nil
}

func TestAddressServiceSet(t *testing.T) {
	repo := &fakeAddressRepo{current: Addresses{LatestURL: "https://old.example.com"}}
	bumper := &fakeBumper{}
	svc := NewAddressService(repo, bumper)
	ctx := context.Background()

	// 读：nil 列表下发为空数组
	got, err := svc.Get(ctx)
	if err != nil || got.BackupURLs == nil || got.Socials == nil {
		t.Fatalf("空列表而不是 null：%#v %v", got, err)
	}

	before, after, version, err := svc.Set(ctx, Addresses{
		Notice:       "  请收藏永久地址  ",
		LatestURL:    "https://new.example.com",
		PermanentURL: "https://perm.example.com",
		BackupURLs:   []string{"https://b1.example.com", "   ", "https://b2.example.com"},
		PublishPage:  "https://pub.example.com",
		SupportURL:   "https://t.me/support",
		Telegram:     "https://t.me/channel",
		Socials:      []Social{{Name: " 微博 ", URL: "https://weibo.com/x"}, {Name: "", URL: ""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if before.LatestURL != "https://old.example.com" || after.LatestURL != "https://new.example.com" {
		t.Fatalf("返回修改前后：%q → %q", before.LatestURL, after.LatestURL)
	}
	if after.Notice != "请收藏永久地址" {
		t.Fatalf("去空白：%q", after.Notice)
	}
	if len(after.BackupURLs) != 2 || len(after.Socials) != 1 || after.Socials[0].Name != "微博" {
		t.Fatalf("空行跳过、名称去空白：%+v", after)
	}
	if version != 1 || bumper.calls != 1 {
		t.Fatalf("保存后站点配置版本 +1：version=%d calls=%d", version, bumper.calls)
	}

	bad := []struct {
		in  Addresses
		msg string
	}{
		{Addresses{Notice: strings.Repeat("字", 201)}, "提醒文字不能超过"},
		{Addresses{LatestURL: "/vip"}, "最新地址要填 http"},
		{Addresses{PermanentURL: "javascript:alert(1)"}, "永久地址"},
		{Addresses{Telegram: "t.me/channel"}, "Telegram"},
		{Addresses{BackupURLs: []string{"https://a.example.com", "https://b.example.com", "https://c.example.com", "https://d.example.com"}}, "备用地址最多 3 个"},
		{Addresses{BackupURLs: []string{"not a url"}}, "备用地址 1"},
		{Addresses{Socials: []Social{{Name: "微博"}}}, "社交账号 1 的名称与链接都要填"},
		{Addresses{Socials: []Social{{URL: "https://weibo.com/x"}}}, "社交账号 1 的名称与链接都要填"},
		{Addresses{Socials: manySocials(11)}, "社交账号最多 10 个"},
	}
	for _, b := range bad {
		_, _, _, err := svc.Set(ctx, b.in)
		wantErr(t, err, errs.ErrBadRequest, b.msg)
	}
	// 校验失败不写库、不动版本
	if repo.current.LatestURL != "https://new.example.com" || bumper.calls != 1 {
		t.Fatalf("校验失败不应写库或改版本：%+v %d", repo.current, bumper.calls)
	}
}

func manySocials(n int) []Social {
	out := make([]Social, n)
	for i := range out {
		out[i] = Social{Name: "号", URL: "https://example.com/x"}
	}
	return out
}
