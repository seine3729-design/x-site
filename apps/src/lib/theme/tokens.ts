/**
 * 把后台配的六个种子色推导成界面要用的 18 个设计令牌，并生成 SSR 时写到 `<html>` 上的属性
 * （docs/04 C.8、docs/specs/W0-6.md）。
 *
 * 为什么推导而不是让后台配 18 个：运营配不出协调的 18 个色；也不写死 5 套完整色板，
 * 否则改一个预设要维护 36 个值。推导规则集中在这里，由 tokens.test.ts 逐个预设校验对比度。
 *
 * 只在服务端用（hooks.server.ts）。客户端组件要的是 settings.ts 里的类型与枚举，不要引这个文件，
 * 否则颜色推导会白白进首屏 JS。
 */
import {
	coverAspect,
	DEFAULT_THEME,
	type ColorKey,
	type ColorScheme,
	type LayoutSettings,
	type ThemeFont,
	type ThemePreset,
	type ThemeSettings
} from './settings.ts';

type Seeds = Record<ColorKey, string>;

/**
 * 预设的深色种子色。与后台预览用的那份（admin/src/features/platform/branding-utils.ts 的
 * PRESET_PALETTES）同名同值：两边不一致的话，后台预览就是在骗运营。
 */
const DARK_SEEDS: Record<ThemePreset, Seeds> = {
	classic: { primary: '#e11d48', accent: '#f59e0b', bg: '#0b0b0f', surface: '#17171f', text: '#f5f5f7', muted: '#9ca3af' },
	midnight: { primary: '#3b82f6', accent: '#22d3ee', bg: '#0a1020', surface: '#141c2f', text: '#f1f5f9', muted: '#94a3b8' },
	forest: { primary: '#10b981', accent: '#84cc16', bg: '#071410', surface: '#11211b', text: '#ecfdf5', muted: '#9ca3af' },
	// 紫调的主色用 violet-600 而不是更亮的 violet-500：#8b5cf6 是中间调，白字 4.44:1、深字更低，
	// 按钮文字怎么选都够不到 AA（tokens.test.ts 会拦住）
	violet: { primary: '#7c3aed', accent: '#ec4899', bg: '#120b1f', surface: '#1e1430', text: '#f5f3ff', muted: '#a1a1aa' },
	neutral: { primary: '#525252', accent: '#737373', bg: '#0c0c0c', surface: '#1a1a1a', text: '#fafafa', muted: '#a3a3a3' }
};

/** 浅色的中性色五个预设共用（与后台预览的 LIGHT_OVERRIDES 一致），只有品牌色与强调色按预设走。 */
const LIGHT_NEUTRALS = { bg: '#f6f6f8', surface: '#ffffff', text: '#18181b', muted: '#6b7280' } as const;

/** 各档背景相对「卡片背景」的明度差（百分点）、描边与品牌浅底的透明度。 */
const RAMP = {
	dark: { raised: 4.5, overlay: 7, sunken: 2.2, subtle: -15, line: 0.08, lineStrong: 0.16, brandSoft: 0.16, brandStrong: -8 },
	light: { raised: -5.5, overlay: 0, sunken: -9, subtle: 11, line: 0.09, lineStrong: 0.18, brandSoft: 0.1, brandStrong: -8 }
} as const;

/** 字体：在按 `:lang()` 选出的本地字体栈前面插一段，不额外加载 Web 字体（docs/04 A.4）。 */
const FONT_PREFIX: Record<ThemeFont, string> = {
	system: '',
	serif: "ui-serif, Georgia, 'Songti SC', 'Noto Serif SC', 'Hiragino Mincho ProN',",
	rounded: "ui-rounded, 'SF Pro Rounded', 'Hiragino Maru Gothic ProN', 'Quicksand',"
};

type Rgb = { r: number; g: number; b: number };

function parseHex(hex: string): Rgb {
	const value = hex.trim().slice(1);
	const full = value.length === 3 ? [...value].map((c) => c + c).join('') : value;
	return {
		r: Number.parseInt(full.slice(0, 2), 16),
		g: Number.parseInt(full.slice(2, 4), 16),
		b: Number.parseInt(full.slice(4, 6), 16)
	};
}

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));

function toHex({ r, g, b }: Rgb): string {
	const part = (v: number) => clamp(Math.round(v), 0, 255).toString(16).padStart(2, '0');
	return `#${part(r)}${part(g)}${part(b)}`;
}

type Hsl = { h: number; s: number; l: number };

function rgbToHsl({ r, g, b }: Rgb): Hsl {
	const red = r / 255;
	const green = g / 255;
	const blue = b / 255;
	const max = Math.max(red, green, blue);
	const min = Math.min(red, green, blue);
	const delta = max - min;
	const l = (max + min) / 2;
	if (delta === 0) {
		return { h: 0, s: 0, l: l * 100 };
	}
	const s = delta / (1 - Math.abs(2 * l - 1));
	let h: number;
	if (max === red) {
		h = ((green - blue) / delta) % 6;
	} else if (max === green) {
		h = (blue - red) / delta + 2;
	} else {
		h = (red - green) / delta + 4;
	}
	return { h: ((h * 60) % 360 + 360) % 360, s: s * 100, l: l * 100 };
}

function hslToRgb({ h, s, l }: Hsl): Rgb {
	const sat = clamp(s, 0, 100) / 100;
	const light = clamp(l, 0, 100) / 100;
	const c = (1 - Math.abs(2 * light - 1)) * sat;
	const hue = (((h % 360) + 360) % 360) / 60;
	const x = c * (1 - Math.abs((hue % 2) - 1));
	const [r1, g1, b1] = (
		hue < 1
			? [c, x, 0]
			: hue < 2
				? [x, c, 0]
				: hue < 3
					? [0, c, x]
					: hue < 4
						? [0, x, c]
						: hue < 5
							? [x, 0, c]
							: [c, 0, x]
	) as [number, number, number];
	const m = light - c / 2;
	return { r: (r1 + m) * 255, g: (g1 + m) * 255, b: (b1 + m) * 255 };
}

/** 在原色相饱和度上加减明度（百分点）。 */
export function shiftLightness(hex: string, delta: number): string {
	const hsl = rgbToHsl(parseHex(hex));
	return toHex(hslToRgb({ ...hsl, l: clamp(hsl.l + delta, 0, 100) }));
}

/** 同色加透明度，用于描边与品牌浅底（底色换了也不用重算）。 */
function alpha(hex: string, value: number): string {
	const { r, g, b } = parseHex(hex);
	return `rgb(${r} ${g} ${b} / ${value})`;
}

function luminance(hex: string): number {
	const { r, g, b } = parseHex(hex);
	const channel = (v: number) => {
		const c = v / 255;
		return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
	};
	return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

/** WCAG 2.2 对比度（1–21）。 */
export function contrast(a: string, b: string): number {
	const la = luminance(a);
	const lb = luminance(b);
	return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

/** 这个底色上该用白字还是深色字。 */
function bestOn(background: string): string {
	return contrast('#ffffff', background) >= contrast('#111118', background) ? '#ffffff' : '#111118';
}

/**
 * 把颜色朝「更亮」或「更暗」推，直到与底色的对比度达标。
 * 运营填的颜色在界面上要当文字用时（品牌色链接、会员色文字）必须可读，而他只看得到填进去的那个色。
 */
function ensureContrast(color: string, background: string, target: number, lighten: boolean): string {
	let current = color;
	for (let step = 0; step < 50 && contrast(current, background) < target; step++) {
		current = shiftLightness(current, lighten ? 2 : -2);
	}
	return contrast(current, background) >= target ? current : bestOn(background);
}

function seedsFor(theme: ThemeSettings, scheme: ColorScheme): Seeds {
	const base = DARK_SEEDS[theme.preset] ?? DARK_SEEDS.classic;
	const seeds: Seeds = scheme === 'light' ? { ...base, ...LIGHT_NEUTRALS } : { ...base };
	for (const [key, value] of Object.entries(theme.colors)) {
		if (value) {
			seeds[key as ColorKey] = value;
		}
	}
	return seeds;
}

/**
 * 一套配色的 18 个令牌。运营直接填的（bg / surface / text / muted / primary / accent）原样使用；
 * 推导出来的保证对比度（正文 4.5:1、次要文字 3:1）。
 */
export function schemeTokens(theme: ThemeSettings, scheme: ColorScheme): Record<string, string> {
	const seeds = seedsFor(theme, scheme);
	const ramp = RAMP[scheme];
	const lighten = scheme === 'dark';
	const { bg: canvas, surface, text: fg, primary: brand, accent: vip } = seeds;
	const muted = ensureContrast(seeds.muted, surface, 4.5, lighten);

	return {
		canvas,
		surface,
		raised: shiftLightness(surface, ramp.raised),
		overlay: shiftLightness(surface, ramp.overlay),
		sunken: shiftLightness(surface, ramp.sunken),
		fg,
		'fg-muted': muted,
		'fg-subtle': ensureContrast(shiftLightness(muted, ramp.subtle), surface, 3, lighten),
		line: alpha(fg, ramp.line),
		'line-strong': alpha(fg, ramp.lineStrong),
		brand,
		'brand-strong': shiftLightness(brand, ramp.brandStrong),
		'brand-fg': ensureContrast(brand, surface, 4.5, lighten),
		'brand-soft': alpha(brand, ramp.brandSoft),
		'on-brand': bestOn(brand),
		vip,
		'vip-fg': ensureContrast(vip, surface, 4.5, lighten),
		'on-vip': bestOn(vip)
	};
}

/**
 * `<html style="…">` 的内容：两套配色（`--d-*` 深色、`--l-*` 浅色）同时注入，
 * 这样深浅色切换只是加减 `<html class="light">`，首帧与切换都不闪（决定 2）。
 * `app.css` 里每个令牌都写成 `var(--d-x, 内置值)`，后端不可用时自动落到内置色板。
 */
export function rootStyle(theme: ThemeSettings, layout: LayoutSettings): string {
	const parts: string[] = [];
	for (const [prefix, scheme] of [
		['d', 'dark'],
		['l', 'light']
	] as const) {
		for (const [name, value] of Object.entries(schemeTokens(theme, scheme))) {
			parts.push(`--${prefix}-${name}:${value}`);
		}
	}
	parts.push(`--card-radius:${theme.radius}px`, `--cover-ratio:${coverAspect(theme.coverRatio)}`);
	if (FONT_PREFIX[theme.font]) {
		parts.push(`--app-font-prefix:${FONT_PREFIX[theme.font]}`);
	}
	if (!layout.mobileTabbar) {
		// 页脚与沉浸式页面的底部留白都是 calc(var(--tabbar-h) + …)，置零就不用改各处样式
		parts.push('--tabbar-h:0rem');
	}
	return parts.join(';');
}

/**
 * SSR 时替换 app.html 里 `<html>` 上的占位：深浅色 class、给内联脚本看的 data 属性、令牌。
 *
 * 值全部来自 settings.ts 校验过的枚举、数字与 #hex，不可能含引号或尖括号；
 * 仍然过一道 escape，避免以后往 theme 里加自由文本字段时出 HTML 注入。
 */
export function rootAttributes(theme: ThemeSettings, layout: LayoutSettings): string {
	const attributes = [`data-scheme="${theme.scheme}"`];
	if (theme.scheme === 'light') {
		// system 按深色出：服务端看不到系统偏好，首帧前的内联脚本会校正
		attributes.push('class="light"');
	}
	if (!theme.allowSchemeToggle) {
		attributes.push('data-scheme-lock');
	}
	attributes.push(`style="${rootStyle(theme, layout)}"`);
	return escape(attributes.join(' '));
}

function escape(value: string): string {
	return value.replace(/[<>`]|&(?![a-z]+;)/gi, '');
}

/** 后端不可用时的注入内容：只给内联脚本定深浅色，颜色走 app.css 的内置兜底。 */
export function fallbackRootAttributes(): string {
	return `data-scheme="${DEFAULT_THEME.scheme}"`;
}
