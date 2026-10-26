/**
 * 剧集角标预设（docs/04 C.9）。
 *
 * 后台「剧资料 → 角标」选一个预设，存进 drama.badge 的是预设 id（如 `hot`）：
 * - 预设：样式由这里决定，文案走 i18n（9 种语言），所以角标也会跟着界面语言变。
 * - 其他值：按「自定义」原样显示（只有后台填的那一种语言），老数据不会丢。
 *
 * 配色写在 app.css 的 `.badge-*`（手写在 `@layer components` 里，不是按需生成的工具类，
 * 所以类名在这里拼出来也不会被 Tailwind 摇掉——构建产物里 13 个 `.badge-*` 都在）。
 */
import type { MessageKey } from '#lib/i18n/translate.ts';
import type { IconName } from '#lib/ui/icons.ts';

export type BadgeStyle = 'hot' | 'new' | 'exclusive' | 'trending' | 'preview' | 'free' | 'finished' | 'vip' | 'custom';

export type ResolvedBadge = {
	style: BadgeStyle;
	/** app.css 里的样式类 */
	cls: string;
	/** 图标只给形状辨识度高、实心也好看的几个预设 */
	icon?: IconName;
	/** 预设的文案键（i18n） */
	labelKey?: MessageKey;
	/** 自定义角标的原文 */
	text?: string;
};

type Preset = {
	style: Exclude<BadgeStyle, 'custom'>;
	icon?: IconName;
	/** 兼容老数据与手输：这些写法也归到本预设（小写比较） */
	aliases: string[];
};

const PRESETS: Preset[] = [
	{ style: 'hot', icon: 'flame', aliases: ['hot', '热播', '热门', '最热', '热'] },
	{ style: 'new', icon: 'sparkles', aliases: ['new', '新剧', '最新', '上新', '新'] },
	{ style: 'exclusive', aliases: ['exclusive', '独家', '独播', '独家首发'] },
	{ style: 'trending', aliases: ['trending', '爆款', '爆火', '霸榜'] },
	{ style: 'preview', aliases: ['preview', 'soon', '预告', '预上线', '即将上线'] },
	{ style: 'free', aliases: ['free', '限免', '免费', '免费看'] },
	{ style: 'finished', aliases: ['finished', 'end', 'ended', '完结', '已完结', '全集'] },
	{ style: 'vip', icon: 'crown', aliases: ['vip', 'svip', '会员', '会员专享'] }
];

/** 后台可选的预设 id，顺序即后台下拉的顺序 */
export const BADGE_STYLES = PRESETS.map((p) => p.style);

const BY_ALIAS = new Map<string, Preset>();
for (const preset of PRESETS) {
	BY_ALIAS.set(preset.style, preset);
	for (const alias of preset.aliases) BY_ALIAS.set(alias, preset);
}

/** 空（含只有空格）返回 null，表示不渲染角标 */
export function resolveBadge(raw: string | null | undefined): ResolvedBadge | null {
	const text = (raw ?? '').trim();
	if (!text) return null;
	const preset = BY_ALIAS.get(text.toLowerCase());
	if (!preset) return { style: 'custom', cls: 'badge-custom', text };
	return {
		style: preset.style,
		cls: `badge-${preset.style}`,
		icon: preset.icon,
		labelKey: `badge.${preset.style}`
	};
}
