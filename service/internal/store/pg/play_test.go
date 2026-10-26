package pg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/errs"
)

// 选源（S1-7）与清单（S1-8）只给前台可见的剧里已转封装的分集：其余情况一律当作不存在。
func TestPlayRepo_PlayableEpisode(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	admin := content.NewService(NewContentRepo(pool))
	repo := NewPlayRepo(pool)
	mark := fmt.Sprintf("pl%d", time.Now().UnixNano())
	t.Cleanup(func() {
		bg := context.Background()
		// 剧删除时分集级联删除，分集引用的媒体对象随后才能删
		_, _ = pool.Exec(bg, `DELETE FROM drama WHERE title LIKE $1`, mark+"%")
		_, _ = pool.Exec(bg, `DELETE FROM media_object WHERE key LIKE $1`, mark+"/%")
	})

	past, future := time.Now().Add(-time.Hour), time.Now().Add(48*time.Hour)
	mkDrama := func(name string, status content.DramaStatus, publishAt *time.Time) int64 {
		d := must(admin.CreateDrama(ctx, content.DramaInput{Title: mark + name}))(t)
		if _, _, err := admin.UpdateDrama(ctx, d.ID, content.DramaInput{Title: d.Title, Status: status, PublishAt: publishAt}); err != nil {
			t.Fatal(err)
		}
		return d.ID
	}
	// mkEpisode 建一集；ready 为 true 时模拟转封装完成（S1-5 写入 media_status、视频对象），返回分集与视频对象编号。
	// kind 为空时不建视频对象。
	mkEpisode := func(dramaID int64, number int32, in content.EpisodeInput, ready bool, kind string) (int64, int64) {
		in.Number = number
		ep := must(admin.CreateEpisode(ctx, dramaID, in))(t)
		if !ready {
			return ep.ID, 0
		}
		var objectID *int64
		if kind != "" {
			id := insertID(t, pool, `INSERT INTO media_object (kind, key, episode_id) VALUES ($1, $2, $3) RETURNING id`,
				kind, fmt.Sprintf("%s/hls/%d/job", mark, ep.ID), ep.ID)
			objectID = &id
		}
		if _, err := pool.Exec(ctx, `UPDATE episode SET media_status = 'ready', video_object_id = $2 WHERE id = $1`, ep.ID, objectID); err != nil {
			t.Fatal(err)
		}
		if objectID == nil {
			return ep.ID, 0
		}
		return ep.ID, *objectID
	}
	episodeOnly := func(epID, _ int64) int64 { return epID }

	visible := mkDrama("可见", content.DramaPublished, &past)
	vipEp, vipObject := mkEpisode(visible, 1, content.EpisodeInput{Access: content.AccessVIP, TrialSec: 30, DurationSec: 120}, true, "hls_dir")
	pending := episodeOnly(mkEpisode(visible, 2, content.EpisodeInput{Access: content.AccessFree}, false, ""))
	noObject := episodeOnly(mkEpisode(visible, 3, content.EpisodeInput{Access: content.AccessFree}, true, ""))
	fileObject := episodeOnly(mkEpisode(visible, 4, content.EpisodeInput{Access: content.AccessFree}, true, "file"))

	got, err := repo.PlayableEpisode(ctx, vipEp)
	if err != nil {
		t.Fatalf("可见剧里已转封装的分集应能选源：%v", err)
	}
	if got.ID != vipEp || got.DramaID != visible || got.Access != content.AccessVIP || got.TrialSec != 30 ||
		got.VideoObjectID != vipObject || got.ObjectKey != fmt.Sprintf("%s/hls/%d/job", mark, vipEp) {
		t.Fatalf("分集字段不对：%+v（视频对象应为 %d）", got, vipObject)
	}

	draft := mkDrama("草稿", content.DramaDraft, nil)
	offline := mkDrama("下架", content.DramaOffline, nil)
	scheduled := mkDrama("定时", content.DramaPublished, &future)
	trashed := mkDrama("回收站", content.DramaPublished, &past)
	cases := map[string]int64{
		"未转封装的分集":       pending,
		"标记可播放但没有视频对象":  noObject,
		"视频对象不是 HLS 目录": fileObject,
		"草稿剧的分集":        episodeOnly(mkEpisode(draft, 1, content.EpisodeInput{Access: content.AccessFree}, true, "hls_dir")),
		"下架剧的分集":        episodeOnly(mkEpisode(offline, 1, content.EpisodeInput{Access: content.AccessFree}, true, "hls_dir")),
		"定时未到点的剧的分集":    episodeOnly(mkEpisode(scheduled, 1, content.EpisodeInput{Access: content.AccessFree}, true, "hls_dir")),
		"回收站中的剧的分集":     episodeOnly(mkEpisode(trashed, 1, content.EpisodeInput{Access: content.AccessFree}, true, "hls_dir")),
		"不存在的分集":        1 << 50,
	}
	if _, err := admin.DeleteDrama(ctx, trashed); err != nil {
		t.Fatal(err)
	}
	for name, id := range cases {
		if _, err := repo.PlayableEpisode(ctx, id); !errors.Is(err, errs.ErrNotFound) {
			t.Errorf("%s：应视为不存在，得到 %v", name, err)
		}
	}
}

// 密钥接口（S1-8）按版本取内容密钥：重新转封装后旧版本仍在库里，清单里的旧地址照常可用。
func TestPlayRepo_MediaKey(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	admin := content.NewService(NewContentRepo(pool))
	repo := NewPlayRepo(pool)
	mark := fmt.Sprintf("pk%d", time.Now().UnixNano())
	// 分集删除时密钥级联删除
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM drama WHERE title LIKE $1`, mark+"%") })

	d := must(admin.CreateDrama(ctx, content.DramaInput{Title: mark}))(t)
	ep := must(admin.CreateEpisode(ctx, d.ID, content.EpisodeInput{Number: 1, Access: content.AccessFree}))(t)
	for ver, wrapped := range map[int32][]byte{1: []byte("k1:old"), 2: []byte("k1:new")} {
		if _, err := pool.Exec(ctx, `INSERT INTO media_key (episode_id, key_ver, wrapped_key) VALUES ($1, $2, $3)`, ep.ID, ver, wrapped); err != nil {
			t.Fatal(err)
		}
	}
	for ver, want := range map[int32]string{1: "k1:old", 2: "k1:new"} {
		got, err := repo.MediaKey(ctx, ep.ID, ver)
		if err != nil || !bytes.Equal(got, []byte(want)) {
			t.Fatalf("第 %d 版：%q %v", ver, got, err)
		}
	}
	for name, args := range map[string][2]int64{"版本不存在": {ep.ID, 3}, "分集不存在": {1 << 50, 1}} {
		if _, err := repo.MediaKey(ctx, args[0], int32(args[1])); !errors.Is(err, errs.ErrNotFound) {
			t.Errorf("%s：应返回 ErrNotFound，得到 %v", name, err)
		}
	}
}
