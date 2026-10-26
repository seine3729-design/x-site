/**
 * 评论区的纯逻辑（docs/04 C.4）：楼层组装、翻页合并、草稿校验、表情码切分、请求地址拼装。
 * 接口只返回扁平列表（每条带 parent_id，没有回复数），一层回复的结构由这里组装。
 */
import type { CommentView, EmojiPack } from '#lib/api/types.ts';

/** 评论内容上限，与后端 maxCommentRunes 一致（超了后端返回 400）。 */
export const COMMENT_MAX = 500;

/** 列表排序。接口只认 latest 与 hot。 */
export const COMMENT_SORTS = ['latest', 'hot'] as const;
export type CommentSort = (typeof COMMENT_SORTS)[number];

/** 举报原因，与后端 reportReasons 枚举一致（docs/02 第 5.6 节）。 */
export const REPORT_REASONS = ['attack', 'vulgar', 'illegal', 'minor', 'ad', 'spoiler', 'other'] as const;
export type ReportReason = (typeof REPORT_REASONS)[number];

/** 一个楼层：顶层评论 + 它的回复（按时间升序，读起来是对话顺序）。 */
export type CommentThread = { root: CommentView; replies: CommentView[] };

/**
 * 把扁平列表组装成楼层。
 * 回复的父评论不在本页时（父评论被删、或还在下一页）按顶层显示，避免内容凭空消失。
 */
export function buildThreads(list: readonly CommentView[]): CommentThread[] {
	const ids = new Set(list.map((c) => c.id));
	const threads: CommentThread[] = [];
	const byID = new Map<number, CommentThread>();
	for (const item of list) {
		if (isRoot(item, ids)) {
			const thread: CommentThread = { root: item, replies: [] };
			threads.push(thread);
			byID.set(item.id, thread);
		}
	}
	for (const item of list) {
		if (isRoot(item, ids)) {
			continue;
		}
		// 回复的回复挂到它所属的顶层楼层（产品上只展示一层）
		const thread = byID.get(item.parent_id!) ?? findThread(threads, item.parent_id!);
		thread?.replies.push(item);
	}
	for (const thread of threads) {
		thread.replies.sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at) || a.id - b.id);
	}
	return threads;
}

/** 顶层评论：没有父评论、父评论不在本页，或指向自己的脏数据（都按顶层显示，不丢内容）。 */
function isRoot(item: CommentView, ids: ReadonlySet<number>): boolean {
	return item.parent_id === undefined || item.parent_id === item.id || !ids.has(item.parent_id);
}

function findThread(threads: readonly CommentThread[], id: number): CommentThread | undefined {
	return threads.find((t) => t.root.id === id || t.replies.some((r) => r.id === id));
}

/**
 * 追加下一页：按 id 去重，保留先出现的那条。
 * 翻页期间别人发了新评论会让游标窗口重叠，同一条可能出现在两页。
 */
export function mergePages(prev: readonly CommentView[], next: readonly CommentView[]): CommentView[] {
	const seen = new Set(prev.map((c) => c.id));
	const merged = [...prev];
	for (const item of next) {
		if (!seen.has(item.id)) {
			seen.add(item.id);
			merged.push(item);
		}
	}
	return merged;
}

/** 把一条新评论放到列表最前（已存在则替换，用于乐观插入后被接口回执覆盖）。 */
export function prependComment(list: readonly CommentView[], item: CommentView): CommentView[] {
	return [item, ...list.filter((c) => c.id !== item.id)];
}

/** 字数按码点算（与后端 runeLen 一致，emoji 不会被算成两个字）。 */
export function contentLength(text: string): number {
	return [...text].length;
}

export type DraftState = 'empty' | 'too-long' | 'ok';

/** 草稿状态：空白（含只有空格换行）不能发，超 500 字不能发。 */
export function draftState(text: string): DraftState {
	const trimmed = text.trim();
	if (trimmed === '') {
		return 'empty';
	}
	return contentLength(trimmed) > COMMENT_MAX ? 'too-long' : 'ok';
}

/** 表情码形如 [微笑]，与后端 cleanEmoji 的约定一致。 */
const EMOJI_CODE = /\[[^[\]\n]{1,30}\]/g;

export type TextSegment = { kind: 'text'; text: string };
export type EmojiSegment = { kind: 'emoji'; code: string; image: string };
export type ContentSegment = TextSegment | EmojiSegment;

/**
 * 把评论正文按表情码切成「纯文本 / 表情」片段，供组件逐段渲染（全程文本插值，不用 {@html}）。
 * 未登记的表情码按原文保留，用户看到的就是后端存下来的内容。
 */
export function splitContent(text: string, emojis: ReadonlyMap<string, string>): ContentSegment[] {
	const segments: ContentSegment[] = [];
	let cursor = 0;
	for (const match of text.matchAll(EMOJI_CODE)) {
		const image = emojis.get(match[0]);
		if (image === undefined) {
			continue;
		}
		if (match.index > cursor) {
			segments.push({ kind: 'text', text: text.slice(cursor, match.index) });
		}
		segments.push({ kind: 'emoji', code: match[0], image });
		cursor = match.index + match[0].length;
	}
	if (cursor < text.length) {
		segments.push({ kind: 'text', text: text.slice(cursor) });
	}
	return segments;
}

/** 表情还没取到时用的空映射（组件里不新建 Map，避免把不可响应的对象塞进 $state）。 */
export const EMPTY_EMOJIS: ReadonlyMap<string, string> = new Map();

/** 表情包列表 → 表情码到图片的映射（同码后出现的以先出现的为准，与后端唯一约束一致）。 */
export function emojiIndex(packs: readonly EmojiPack[]): Map<string, string> {
	const map = new Map<string, string>();
	for (const pack of packs) {
		for (const emoji of pack.emojis) {
			if (!map.has(emoji.code)) {
				map.set(emoji.code, emoji.image);
			}
		}
	}
	return map;
}

/**
 * 评论列表的请求地址。查询串按键排序、默认值省略：同一组条件只有一个地址，便于命中缓存。
 * 注意路径与查询串参与报文加密的 AAD，拼好后不能再被改写。
 */
export function commentsPath(episodeID: number, sort: CommentSort, cursor?: string | null, limit = 20): string {
	const search = new URLSearchParams();
	if (cursor) {
		search.set('cursor', cursor);
	}
	search.set('limit', String(limit));
	if (sort !== 'latest') {
		search.set('sort', sort);
	}
	return `/api/v1/episodes/${episodeID}/comments?${[...search].sort(([a], [b]) => a.localeCompare(b)).map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join('&')}`;
}

/** 这条评论是不是当前注册用户发的（游客的评论不下发标识，认不出来，只能靠本次会话发过的记录）。 */
export function isMine(item: CommentView, userID: number | undefined, postedIDs: readonly number[]): boolean {
	if (userID !== undefined && item.author.user_id === userID) {
		return true;
	}
	return postedIDs.includes(item.id);
}
