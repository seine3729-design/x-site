/**
 * 后台「白标」里 theme / layout 两段的解析（docs/06 第 16 节，S3-3）。
 *
 * 枚举与默认值必须与 service/internal/domain/platform/branding.go 的 defaultXxx 一致：
 * 后端对坏值返回 400，所以存下来的都是合法值；这里再白名单一次是为了「配置缺失（新站点没保存过）」
 * 和「后端新增了取值而前台还没发版」两种情况——认不出的取值按默认值渲染，不让页面歪掉。
 *
 * 本文件是纯函数、不含颜色推导，客户端组件可以安全引用其中的类型。
 */

export type ColorScheme = 'dark' | 'light';
export type SchemePreference = ColorScheme | 'system';

export const THEME_PRESETS = ['classic', 'midnight', 'forest', 'violet', 'neutral'] as const;
export type ThemePreset = (typeof THEME_PRESETS)[number];

export const COVER_RATIOS = ['2:3', '3:4', '16:9'] as const;
export type CoverRatio = (typeof COVER_RATIOS)[number];

export const THEME_FONTS = ['system', 'serif', 'rounded'] as const;
export type ThemeFont = (typeof THEME_FONTS)[number];

export const HEADER_STYLES = ['classic', 'centered', 'pill'] as const;
export type HeaderStyle = (typeof HEADER_STYLES)[number];

export const CARD_STYLES = ['below', 'overlay'] as const;
export type CardStyle = (typeof CARD_STYLES)[number];

export const PLAY_MODES = ['mobile_immersive', 'all_immersive', 'all_detail'] as const;
export type PlayMode = (typeof PLAY_MODES)[number];

/** 可被运营覆盖的六个种子色（docs/06 第 16 节的「配色」卡）。 */
export const COLOR_KEYS = ['primary', 'accent', 'bg', 'surface', 'text', 'muted'] as const;
export type ColorKey = (typeof COLOR_KEYS)[number];

export type ThemeSettings = {
	preset: ThemePreset;
	/** 默认深浅色；system 表示跟随系统 */
	scheme: SchemePreference;
	/** 为假时前台不显示深浅色切换，并忽略本机已存的偏好 */
	allowSchemeToggle: boolean;
	/** 运营填的覆盖色（#rgb / #rrggbb），没填的键不出现 */
	colors: Partial<Record<ColorKey, string>>;
	/** 卡片圆角（px，0–32） */
	radius: number;
	coverRatio: CoverRatio;
	font: ThemeFont;
};

export type LayoutSettings = {
	headerStyle: HeaderStyle;
	cardStyle: CardStyle;
	playMode: PlayMode;
	mobileTabbar: boolean;
};

export const MAX_RADIUS = 32;

export const DEFAULT_THEME: ThemeSettings = {
	preset: 'classic',
	scheme: 'dark',
	allowSchemeToggle: true,
	colors: {},
	radius: 12,
	coverRatio: '2:3',
	font: 'system'
};

export const DEFAULT_LAYOUT: LayoutSettings = {
	headerStyle: 'classic',
	cardStyle: 'below',
	playMode: 'mobile_immersive',
	mobileTabbar: true
};

const HEX = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i;

function pick<T extends string>(value: unknown, allowed: readonly T[], fallback: T): T {
	return typeof value === 'string' && (allowed as readonly string[]).includes(value) ? (value as T) : fallback;
}

function bool(value: unknown, fallback: boolean): boolean {
	return typeof value === 'boolean' ? value : fallback;
}

function record(value: unknown): Record<string, unknown> {
	return value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

/** 解析 site_config.theme；任何缺失或认不出的字段取默认值。 */
export function normalizeTheme(raw: unknown): ThemeSettings {
	const source = record(raw);
	const colors: Partial<Record<ColorKey, string>> = {};
	const given = record(source.colors);
	for (const key of COLOR_KEYS) {
		const value = given[key];
		if (typeof value === 'string' && HEX.test(value.trim())) {
			colors[key] = value.trim().toLowerCase();
		}
	}
	const radius = typeof source.radius === 'number' && Number.isFinite(source.radius) ? source.radius : DEFAULT_THEME.radius;
	return {
		preset: pick(source.preset, THEME_PRESETS, DEFAULT_THEME.preset),
		scheme: pick(source.color_scheme, ['dark', 'light', 'system'] as const, DEFAULT_THEME.scheme),
		allowSchemeToggle: bool(source.allow_scheme_toggle, DEFAULT_THEME.allowSchemeToggle),
		colors,
		radius: Math.min(MAX_RADIUS, Math.max(0, Math.round(radius))),
		coverRatio: pick(source.cover_ratio, COVER_RATIOS, DEFAULT_THEME.coverRatio),
		font: pick(source.font, THEME_FONTS, DEFAULT_THEME.font)
	};
}

/** 解析 site_config.layout；任何缺失或认不出的字段取默认值。 */
export function normalizeLayout(raw: unknown): LayoutSettings {
	const source = record(raw);
	return {
		headerStyle: pick(source.header_style, HEADER_STYLES, DEFAULT_LAYOUT.headerStyle),
		cardStyle: pick(source.card_style, CARD_STYLES, DEFAULT_LAYOUT.cardStyle),
		playMode: pick(source.play_mode, PLAY_MODES, DEFAULT_LAYOUT.playMode),
		mobileTabbar: bool(source.mobile_tabbar, DEFAULT_LAYOUT.mobileTabbar)
	};
}

/** 封面比例转 CSS `aspect-ratio` 的值（`2:3` → `2 / 3`）。 */
export function coverAspect(ratio: CoverRatio): string {
	const [w, h] = ratio.split(':');
	return `${w} / ${h}`;
}

/** 按封面比例算出占位用的宽高（CLS）：宽度固定 300，高度按比例取整。 */
export function coverSize(ratio: CoverRatio): { width: number; height: number } {
	const [w, h] = ratio.split(':').map(Number);
	return { width: 300, height: Math.round((300 * h) / w) };
}
