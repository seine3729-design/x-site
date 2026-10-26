import { describe, expect, it } from 'vitest';
import { CATALOGS } from '#lib/server/i18n-catalogs.ts';
import { BADGE_STYLES, resolveBadge } from './badge.ts';

describe('剧集角标', () => {
	it('空值不渲染', () => {
		expect(resolveBadge('')).toBeNull();
		expect(resolveBadge('   ')).toBeNull();
		expect(resolveBadge(null)).toBeNull();
		expect(resolveBadge(undefined)).toBeNull();
	});

	it('预设 id 解析成对应样式，文案走 i18n', () => {
		for (const style of BADGE_STYLES) {
			const badge = resolveBadge(style);
			expect(badge).toMatchObject({ style, cls: `badge-${style}`, labelKey: `badge.${style}` });
			expect(badge?.text).toBeUndefined();
		}
	});

	it('每个预设的文案在 9 种语言里都有', () => {
		for (const [locale, catalog] of Object.entries(CATALOGS)) {
			for (const style of BADGE_STYLES) {
				expect(catalog[`badge.${style}`], `${locale} 缺少 badge.${style}`).toBeTruthy();
			}
		}
	});

	it('老数据的中英文写法归到预设（大小写、空格不敏感）', () => {
		expect(resolveBadge('HOT')?.style).toBe('hot');
		expect(resolveBadge(' 热播 ')?.style).toBe('hot');
		expect(resolveBadge('独播')?.style).toBe('exclusive');
		expect(resolveBadge('已完结')?.style).toBe('finished');
		expect(resolveBadge('会员专享')?.style).toBe('vip');
	});

	it('认不出的文案按自定义原样显示', () => {
		const badge = resolveBadge('年度压轴');
		expect(badge).toEqual({ style: 'custom', cls: 'badge-custom', text: '年度压轴' });
	});

	it('只有热播、新剧、会员带图标，其他只有文字', () => {
		expect(resolveBadge('hot')?.icon).toBe('flame');
		expect(resolveBadge('new')?.icon).toBe('sparkles');
		expect(resolveBadge('vip')?.icon).toBe('crown');
		expect(resolveBadge('trending')?.icon).toBeUndefined();
		expect(resolveBadge('自定义')?.icon).toBeUndefined();
	});
});
