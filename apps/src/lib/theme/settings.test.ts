import { describe, expect, it } from 'vitest';
import {
	coverAspect,
	coverSize,
	DEFAULT_LAYOUT,
	DEFAULT_THEME,
	normalizeLayout,
	normalizeTheme
} from './settings.ts';

describe('normalizeTheme', () => {
	it('配置为空时与后端默认值一致', () => {
		// 对应 service/internal/domain/platform/branding.go 的 defaultPreset / defaultColorScheme / …
		expect(normalizeTheme({})).toEqual(DEFAULT_THEME);
		expect(normalizeTheme(undefined)).toEqual(DEFAULT_THEME);
		expect(normalizeTheme('坏数据')).toEqual(DEFAULT_THEME);
	});

	it('读取后台保存的各段', () => {
		const theme = normalizeTheme({
			preset: 'midnight',
			color_scheme: 'light',
			allow_scheme_toggle: false,
			colors: { primary: '#FF0033', bg: '#fff' },
			radius: 4,
			cover_ratio: '16:9',
			font: 'serif'
		});
		expect(theme).toEqual({
			preset: 'midnight',
			scheme: 'light',
			allowSchemeToggle: false,
			colors: { primary: '#ff0033', bg: '#fff' },
			radius: 4,
			coverRatio: '16:9',
			font: 'serif'
		});
	});

	it('认不出的枚举取默认值', () => {
		// 后端加了新取值而前台还没发版时，页面按默认值渲染而不是歪掉
		const theme = normalizeTheme({ preset: 'neon', color_scheme: 'auto', cover_ratio: '1:1', font: 'comic' });
		expect(theme.preset).toBe('classic');
		expect(theme.scheme).toBe('dark');
		expect(theme.coverRatio).toBe('2:3');
		expect(theme.font).toBe('system');
	});

	it('丢掉不是色值的覆盖色与不在清单里的键', () => {
		const theme = normalizeTheme({ colors: { primary: 'red', accent: '#12345', bg: '#abc', nope: '#fff' } });
		expect(theme.colors).toEqual({ bg: '#abc' });
	});

	it('圆角取整并限制在 0–32px', () => {
		expect(normalizeTheme({ radius: -5 }).radius).toBe(0);
		expect(normalizeTheme({ radius: 99 }).radius).toBe(32);
		expect(normalizeTheme({ radius: 10.6 }).radius).toBe(11);
		expect(normalizeTheme({ radius: Number.NaN }).radius).toBe(DEFAULT_THEME.radius);
	});
});

describe('normalizeLayout', () => {
	it('配置为空时与后端默认值一致', () => {
		expect(normalizeLayout({})).toEqual(DEFAULT_LAYOUT);
	});

	it('读取后台保存的布局', () => {
		expect(normalizeLayout({ header_style: 'pill', card_style: 'overlay', play_mode: 'all_immersive', mobile_tabbar: false })).toEqual({
			headerStyle: 'pill',
			cardStyle: 'overlay',
			playMode: 'all_immersive',
			mobileTabbar: false
		});
	});

	it('认不出的枚举取默认值', () => {
		expect(normalizeLayout({ header_style: 'split', card_style: 'side', play_mode: 'cinema' })).toEqual(DEFAULT_LAYOUT);
	});
});

describe('封面比例', () => {
	it('转成 CSS aspect-ratio', () => {
		expect(coverAspect('2:3')).toBe('2 / 3');
		expect(coverAspect('16:9')).toBe('16 / 9');
	});

	it('占位宽高按比例算（CLS）', () => {
		expect(coverSize('2:3')).toEqual({ width: 300, height: 450 });
		expect(coverSize('3:4')).toEqual({ width: 300, height: 400 });
		expect(coverSize('16:9')).toEqual({ width: 300, height: 169 });
	});
});
