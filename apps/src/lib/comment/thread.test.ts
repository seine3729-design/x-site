import { describe, expect, it } from 'vitest';
import type { CommentView, EmojiPack } from '#lib/api/types.ts';
import {
	buildThreads,
	commentsPath,
	contentLength,
	draftState,
	emojiIndex,
	isMine,
	mergePages,
	prependComment,
	splitContent
} from './thread.ts';

function comment(id: number, extra: Partial<CommentView> = {}): CommentView {
	return {
		id,
		content: `内容 ${id}`,
		image_count: 0,
		like_count: 0,
		created_at: `2026-10-26T10:0${id % 10}:00Z`,
		author: { is_guest: false, user_id: 1, nickname: '小鱼' },
		...extra
	};
}

describe('楼层组装', () => {
	it('回复挂到父评论下，按时间升序', () => {
		const threads = buildThreads([
			comment(3, { parent_id: 1, created_at: '2026-10-26T12:00:00Z' }),
			comment(1),
			comment(2, { parent_id: 1, created_at: '2026-10-26T11:00:00Z' }),
			comment(4)
		]);
		expect(threads.map((t) => t.root.id)).toEqual([1, 4]);
		expect(threads[0]!.replies.map((r) => r.id)).toEqual([2, 3]);
		expect(threads[1]!.replies).toEqual([]);
	});

	it('父评论不在本页的回复按顶层显示，不丢内容', () => {
		const threads = buildThreads([comment(9, { parent_id: 404 }), comment(10)]);
		expect(threads.map((t) => t.root.id)).toEqual([9, 10]);
	});

	it('回复的回复并到同一个顶层楼层，不丢也不无限嵌套', () => {
		const threads = buildThreads([comment(1), comment(2, { parent_id: 1 }), comment(3, { parent_id: 2 })]);
		expect(threads).toHaveLength(1);
		expect(threads[0]!.replies.map((r) => r.id)).toEqual([2, 3]);
	});

	it('自己指向自己的脏数据不会把自己当成自己的回复', () => {
		const threads = buildThreads([comment(1, { parent_id: 1 })]);
		expect(threads).toHaveLength(1);
		expect(threads[0]!.replies).toEqual([]);
	});
});

describe('翻页与插入', () => {
	it('两页重叠的同一条只留一条，顺序不变', () => {
		const merged = mergePages([comment(5), comment(4)], [comment(4), comment(3)]);
		expect(merged.map((c) => c.id)).toEqual([5, 4, 3]);
	});

	it('下一页自身重复的条目也只留一条', () => {
		expect(mergePages([], [comment(2), comment(2), comment(1)]).map((c) => c.id)).toEqual([2, 1]);
	});

	it('新评论放到最前，重复 id 被替换', () => {
		const list = [comment(1, { content: '旧' }), comment(2)];
		const next = prependComment(list, comment(1, { content: '新' }));
		expect(next.map((c) => c.id)).toEqual([1, 2]);
		expect(next[0]!.content).toBe('新');
	});
});

describe('草稿校验', () => {
	it('空白不能发，超 500 字不能发', () => {
		expect(draftState('   \n ')).toBe('empty');
		expect(draftState('好看')).toBe('ok');
		expect(draftState('字'.repeat(500))).toBe('ok');
		expect(draftState('字'.repeat(501))).toBe('too-long');
	});

	it('字数按码点算，emoji 只算一个字', () => {
		expect(contentLength('👍🏻')).toBe(2); // 手势 + 肤色修饰符各一个码点
		expect(contentLength('😀')).toBe(1);
		expect(draftState(`${'字'.repeat(500)}  `)).toBe('ok'); // 首尾空白不计入
	});
});

describe('表情码渲染', () => {
	const packs: EmojiPack[] = [
		{ id: 1, name: '常用', emojis: [{ code: '[微笑]', image: 'img/emoji/smile.webp' }] },
		{ id: 2, name: '追剧', emojis: [{ code: '[催更]', image: 'img/emoji/urge.webp' }, { code: '[微笑]', image: 'dup.webp' }] }
	];
	const index = emojiIndex(packs);

	it('同一个表情码取先登记的那张图', () => {
		expect(index.get('[微笑]')).toBe('img/emoji/smile.webp');
		expect(index.size).toBe(2);
	});

	it('按表情码切成文本与表情片段', () => {
		expect(splitContent('太好看了[微笑]快[催更]', index)).toEqual([
			{ kind: 'text', text: '太好看了' },
			{ kind: 'emoji', code: '[微笑]', image: 'img/emoji/smile.webp' },
			{ kind: 'text', text: '快' },
			{ kind: 'emoji', code: '[催更]', image: 'img/emoji/urge.webp' }
		]);
	});

	it('未登记的表情码与方括号文本按原文保留', () => {
		expect(splitContent('[未知表情]先放着', index)).toEqual([{ kind: 'text', text: '[未知表情]先放着' }]);
		expect(splitContent('', index)).toEqual([]);
	});
});

describe('列表地址', () => {
	it('默认排序省略、键排序，同一组条件只有一个地址', () => {
		expect(commentsPath(12, 'latest')).toBe('/api/v1/episodes/12/comments?limit=20');
		expect(commentsPath(12, 'hot', null, 20)).toBe('/api/v1/episodes/12/comments?limit=20&sort=hot');
	});

	it('游标与特殊字符被转义', () => {
		expect(commentsPath(12, 'hot', '3:88', 10)).toBe('/api/v1/episodes/12/comments?cursor=3%3A88&limit=10&sort=hot');
	});
});

describe('判断是不是自己发的', () => {
	it('注册用户按 user_id 匹配，游客只认本次会话发过的', () => {
		const mine = comment(1, { author: { is_guest: false, user_id: 7, nickname: '我' } });
		const other = comment(2, { author: { is_guest: false, user_id: 8, nickname: '他' } });
		const guest = comment(3, { author: { is_guest: true, nickname: '游客' } });
		expect(isMine(mine, 7, [])).toBe(true);
		expect(isMine(other, 7, [])).toBe(false);
		expect(isMine(guest, undefined, [])).toBe(false);
		expect(isMine(guest, undefined, [3])).toBe(true);
	});
});
