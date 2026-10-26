import { describe, expect, it } from 'vitest';
import { DEFAULT_LAYOUT, normalizeTheme, THEME_PRESETS, type ColorScheme, type ThemeSettings } from './settings.ts';
import { contrast, rootAttributes, rootStyle, schemeTokens, shiftLightness } from './tokens.ts';

const SCHEMES: ColorScheme[] = ['dark', 'light'];
const theme = (partial: Partial<ThemeSettings> = {}) => ({ ...normalizeTheme({}), ...partial });

describe('颜色工具', () => {
	it('对比度按 WCAG 2.2 算', () => {
		expect(contrast('#ffffff', '#000000')).toBeCloseTo(21, 2);
		expect(contrast('#ffffff', '#ffffff')).toBeCloseTo(1, 5);
		// 常被引用的基准值：#777 配白底刚好 4.47:1（AA 的边界）
		expect(contrast('#777777', '#ffffff')).toBeCloseTo(4.48, 1);
	});

	it('明度加减保留色相，并在 0–100 截断', () => {
		expect(shiftLightness('#808080', 10)).toBe('#9a9a9a');
		expect(shiftLightness('#ffffff', 10)).toBe('#ffffff');
		expect(shiftLightness('#000000', -10)).toBe('#000000');
		expect(shiftLightness('#abc', 0)).toBe('#aabbcc');
	});
});

describe('预设色板的对比度（WCAG AA）', () => {
	for (const preset of THEME_PRESETS) {
		for (const scheme of SCHEMES) {
			it(`${preset} · ${scheme}`, () => {
				const t = schemeTokens(theme({ preset }), scheme);
				expect(contrast(t.fg, t.canvas)).toBeGreaterThanOrEqual(4.5);
				expect(contrast(t.fg, t.surface)).toBeGreaterThanOrEqual(4.5);
				expect(contrast(t['fg-muted'], t.surface)).toBeGreaterThanOrEqual(4.5);
				// 次要文字（集数、年份）按大字/辅助信息的 3:1
				expect(contrast(t['fg-subtle'], t.surface)).toBeGreaterThanOrEqual(3);
				expect(contrast(t['brand-fg'], t.surface)).toBeGreaterThanOrEqual(4.5);
				expect(contrast(t['vip-fg'], t.surface)).toBeGreaterThanOrEqual(4.5);
				expect(contrast(t['on-brand'], t.brand)).toBeGreaterThanOrEqual(4.5);
				expect(contrast(t['on-vip'], t.vip)).toBeGreaterThanOrEqual(4.5);
			});
		}
	}

	it('四档背景从卡片背景依次分层（深色变亮、浅色变暗）', () => {
		const dark = schemeTokens(theme(), 'dark');
		expect(new Set([dark.canvas, dark.surface, dark.raised, dark.overlay]).size).toBe(4);
		expect(contrast(dark.raised, dark.canvas)).toBeGreaterThan(contrast(dark.surface, dark.canvas));

		const light = schemeTokens(theme(), 'light');
		expect(contrast(light.raised, light.fg)).toBeLessThan(contrast(light.surface, light.fg));
	});
});

describe('运营自定义颜色', () => {
	it('填进去的六个种子色原样使用', () => {
		const t = schemeTokens(theme({ colors: { primary: '#ff0033', bg: '#101010', surface: '#202020', text: '#fafafa' } }), 'dark');
		expect(t.brand).toBe('#ff0033');
		expect(t.canvas).toBe('#101010');
		expect(t.surface).toBe('#202020');
		expect(t.fg).toBe('#fafafa');
	});

	it('极端色值下推导出来的文字色仍然可读', () => {
		// 亮黄做主色：白字在上面看不见，必须自动翻成深色字；品牌文字色要压暗才能在白卡上读
		for (const scheme of SCHEMES) {
			const t = schemeTokens(theme({ colors: { primary: '#ffff00', accent: '#00ffff' } }), scheme);
			expect(contrast(t['on-brand'], t.brand)).toBeGreaterThanOrEqual(4.5);
			expect(contrast(t['on-vip'], t.vip)).toBeGreaterThanOrEqual(4.5);
			expect(contrast(t['brand-fg'], t.surface)).toBeGreaterThanOrEqual(4.5);
			expect(contrast(t['vip-fg'], t.surface)).toBeGreaterThanOrEqual(4.5);
		}
	});

	it('次要文字色填得太浅也兜到 4.5:1', () => {
		const t = schemeTokens(theme({ colors: { muted: '#f8f8f8' } }), 'light');
		expect(contrast(t['fg-muted'], t.surface)).toBeGreaterThanOrEqual(4.5);
	});
});

describe('注入到 <html> 的内容', () => {
	it('两套配色都在，深色用 --d-、浅色用 --l-', () => {
		const style = rootStyle(theme(), DEFAULT_LAYOUT);
		expect(style).toContain('--d-canvas:#0b0b0f');
		expect(style).toContain('--l-canvas:#f6f6f8');
		expect(style.match(/--d-/g)).toHaveLength(18);
		expect(style.match(/--l-/g)).toHaveLength(18);
	});

	it('圆角、封面比例一起注入；系统字体不注入字体前缀', () => {
		const style = rootStyle(theme({ radius: 4, coverRatio: '16:9' }), DEFAULT_LAYOUT);
		expect(style).toContain('--card-radius:4px');
		expect(style).toContain('--cover-ratio:16 / 9');
		expect(style).not.toContain('--app-font-prefix');
	});

	it('衬线字体插在本地字体栈前面，并以逗号结尾', () => {
		const style = rootStyle(theme({ font: 'serif' }), DEFAULT_LAYOUT);
		expect(style).toMatch(/--app-font-prefix:ui-serif,.+,$/);
	});

	it('关掉手机底栏时底栏高度置零', () => {
		expect(rootStyle(theme(), DEFAULT_LAYOUT)).not.toContain('--tabbar-h');
		expect(rootStyle(theme(), { ...DEFAULT_LAYOUT, mobileTabbar: false })).toContain('--tabbar-h:0rem');
	});

	it('默认浅色时 SSR 就带上 light class，跟随系统则不带', () => {
		expect(rootAttributes(theme({ scheme: 'light' }), DEFAULT_LAYOUT)).toContain('class="light"');
		expect(rootAttributes(theme({ scheme: 'system' }), DEFAULT_LAYOUT)).not.toContain('class="light"');
		expect(rootAttributes(theme({ scheme: 'system' }), DEFAULT_LAYOUT)).toContain('data-scheme="system"');
	});

	it('禁止切换深浅色时带上锁定标记', () => {
		expect(rootAttributes(theme({ allowSchemeToggle: false }), DEFAULT_LAYOUT)).toContain('data-scheme-lock');
		expect(rootAttributes(theme(), DEFAULT_LAYOUT)).not.toContain('data-scheme-lock');
	});

	it('属性串里不含能提前闭合标签的字符', () => {
		const attributes = rootAttributes(theme({ font: 'rounded', colors: { primary: '#abc' } }), DEFAULT_LAYOUT);
		expect(attributes).not.toMatch(/[<>`]/);
		expect(attributes.match(/"/g)).toHaveLength(4); // data-scheme 与 style 各一对引号
	});
});
